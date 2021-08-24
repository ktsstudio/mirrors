/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mirrorsv1alpha1 "github.com/ktsstudio/mirrors/api/v1alpha1"
)

var (
	managedByMirrorAnnotation = "mirrors.kts.studio/owned-by"
	secretOwnerKey            = ".metadata.controller"
	apiGVStr                  = mirrorsv1alpha1.GroupVersion.String()
	mirrorsFinalizerName      = "mirrors.kts.studio/finalizer"

	notManagedByMirror = errors.New("resource is not managed by the Mirror")
)

// SecretMirrorReconciler reconciles a SecretMirror object
type SecretMirrorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=mirrors.kts.studio,resources=secretmirrors,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mirrors.kts.studio,resources=secretmirrors/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mirrors.kts.studio,resources=secretmirrors/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;watch;create;update;patch;delete;list

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SecretMirror object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *SecretMirrorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var secretMirror mirrorsv1alpha1.SecretMirror
	if err := r.Get(ctx, req.NamespacedName, &secretMirror); err != nil {
		logger.Error(err, "unable to fetch SecretMirror")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// examine DeletionTimestamp to determine if object is under deletion
	if secretMirror.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !containsString(secretMirror.GetFinalizers(), mirrorsFinalizerName) {
			controllerutil.AddFinalizer(&secretMirror, mirrorsFinalizerName)
			if err := r.Update(ctx, &secretMirror); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if containsString(secretMirror.GetFinalizers(), mirrorsFinalizerName) {
			// our finalizer is present, so lets handle any external dependency
			if err := r.deleteExternalResources(ctx, &secretMirror); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return ctrl.Result{}, err
			}
			logger.Info("deleted managed objects")

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(&secretMirror, mirrorsFinalizerName)
			if err := r.Update(ctx, &secretMirror); err != nil {
				return ctrl.Result{}, err
			}
		}

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	if secretMirror.Status.MirrorStatus == "" {
		if err := r.setStatePending(ctx, &secretMirror, true); err != nil {
			return ctrl.Result{}, err
		}
	}

	sourceSecretName := types.NamespacedName{
		Namespace: secretMirror.Spec.Source.Namespace,
		Name:      secretMirror.Spec.Source.Name,
	}
	ownSecretName := makeOwnSecretName(&secretMirror)

	if ownSecretName.Namespace == sourceSecretName.Namespace && ownSecretName.Name == sourceSecretName.Name {
		// if SecretMirror deployed into the source
		secretMirror.Status.MirrorStatus = mirrorsv1alpha1.MirrorStatusActive
		secretMirror.Status.LastSyncTime = metav1.Now()
		if err := r.Status().Update(ctx, &secretMirror); err != nil {
			return ctrl.Result{}, err
		}
	}

	var sourceSecret v1.Secret
	if err := r.Get(ctx, sourceSecretName, &sourceSecret); err != nil {
		logger.Error(err, "unable to find source secret, retrying in 1 minute")
		if err := r.setStatePending(ctx, &secretMirror, false); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{
			RequeueAfter: 1 * time.Minute,
		}, client.IgnoreNotFound(err)
	}

	var ownSecret v1.Secret
	ownSecretFound := true
	if err := r.Get(ctx, ownSecretName, &ownSecret); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("own secret not found - will create one")
			ownSecretFound = false
		} else {
			logger.Error(err, "error getting own secret")

			secretMirror.Status.MirrorStatus = mirrorsv1alpha1.MirrorStatusError
			_ = r.Status().Update(ctx, &secretMirror)

			return ctrl.Result{}, err
		}
	}

	managedByMirrorValue := getManagedByMirrorValue(req.Namespace, req.Name)
	if ownSecretFound {
		// check annotations on found secret and make sure that we created it
		value := ownSecret.Annotations[managedByMirrorAnnotation]
		if value != managedByMirrorValue {
			logger.Error(notManagedByMirror,
				fmt.Sprintf("secret %s/%s is not managed by SecretMirror %s",
					ownSecret.Namespace, ownSecret.Name, secretMirror.Name))

			secretMirror.Status.MirrorStatus = mirrorsv1alpha1.MirrorStatusError
			_ = r.Status().Update(ctx, &secretMirror)

			return ctrl.Result{
				RequeueAfter: secretMirror.PollPeriodDuration(),
			}, nil
		}
	} else {
		ownSecret = v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:        sourceSecretName.Name,
				Namespace:   req.Namespace,
				Labels:      make(map[string]string),
				Annotations: make(map[string]string),
			},
			Type: sourceSecret.Type,
		}
	}
	if !secretDiffer(&sourceSecret, &ownSecret) {
		logger.Info("secrets are identical")
		if secretMirror.Status.MirrorStatus != mirrorsv1alpha1.MirrorStatusActive {
			secretMirror.Status.MirrorStatus = mirrorsv1alpha1.MirrorStatusActive
			secretMirror.Status.LastSyncTime = metav1.Now()
			if err := r.Status().Update(ctx, &secretMirror); err != nil {
				logger.Error(err, "unable to update SecretMirror status")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{
			RequeueAfter: secretMirror.PollPeriodDuration(),
		}, nil
	}

	copySecret(&sourceSecret, &ownSecret)
	ownSecret.Annotations[managedByMirrorAnnotation] = managedByMirrorValue

	if ownSecretFound {
		if err := r.Update(ctx, &ownSecret); err != nil {
			logger.Error(err, "unable to update own secret for SecretMirror", "secret", ownSecret)

			secretMirror.Status.MirrorStatus = mirrorsv1alpha1.MirrorStatusError
			_ = r.Status().Update(ctx, &secretMirror)

			return ctrl.Result{
				RequeueAfter: 1 * time.Minute,
			}, err
		}
	} else {
		if err := r.Create(ctx, &ownSecret); err != nil {
			logger.Error(err, "unable to create own secret for SecretMirror", "secret", ownSecret)

			secretMirror.Status.MirrorStatus = mirrorsv1alpha1.MirrorStatusError
			_ = r.Status().Update(ctx, &secretMirror)

			return ctrl.Result{
				RequeueAfter: 1 * time.Minute,
			}, err
		}
	}

	logger.Info(fmt.Sprintf("successfully mirrored secret %s/%s to %s/%s",
		sourceSecret.Namespace, sourceSecret.Name, ownSecret.Namespace, ownSecret.Name))

	secretMirror.Status.MirrorStatus = mirrorsv1alpha1.MirrorStatusActive
	secretMirror.Status.LastSyncTime = metav1.Now()
	if err := r.Status().Update(ctx, &secretMirror); err != nil {
		logger.Error(err, "unable to update SecretMirror status")
		return ctrl.Result{}, err
	}
	return ctrl.Result{
		RequeueAfter: secretMirror.PollPeriodDuration(),
	}, nil
}

func (r *SecretMirrorReconciler) setStatePending(ctx context.Context, secretMirror *mirrorsv1alpha1.SecretMirror, zeroDate bool) error {
	if secretMirror.Status.MirrorStatus == mirrorsv1alpha1.MirrorStatusPending {
		return nil
	}

	secretMirror.Status.MirrorStatus = mirrorsv1alpha1.MirrorStatusPending
	if zeroDate || secretMirror.Status.LastSyncTime.IsZero() {
		secretMirror.Status.LastSyncTime = metav1.Unix(0, 0)
	}
	if err := r.Status().Update(ctx, secretMirror); err != nil {
		return err
	}
	return nil
}

func makeOwnSecretName(secretMirror *mirrorsv1alpha1.SecretMirror) types.NamespacedName {
	return types.NamespacedName{
		Namespace: secretMirror.Namespace,
		Name:      secretMirror.Spec.Source.Name,
	}
}

func secretDiffer(src, dest *v1.Secret) bool {
	if len(src.Labels) != len(dest.Labels) {
		return true
	}

	for k := range src.Labels {
		if src.Labels[k] != dest.Labels[k] {
			return true
		}
	}

	if len(src.Data) != len(dest.Data) {
		return true
	}

	for k := range src.Data {
		if bytes.Compare(src.Data[k], dest.Data[k]) != 0 {
			return true
		}
	}
	return false
}

func copySecret(src, dest *v1.Secret) {
	for k, v := range src.Labels {
		dest.Labels[k] = v
	}
	for k, v := range src.Annotations {
		dest.Annotations[k] = v
	}
	dest.Data = make(map[string][]byte)
	for k, v := range src.Data {
		dataCopy := make([]byte, len(v))
		copy(dataCopy, v)
		dest.Data[k] = dataCopy
	}
}

func getManagedByMirrorValue(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

func (r *SecretMirrorReconciler) deleteExternalResources(ctx context.Context, secretMirror *mirrorsv1alpha1.SecretMirror) error {
	var ownSecret v1.Secret
	if err := r.Get(ctx, makeOwnSecretName(secretMirror), &ownSecret); err != nil {
		return client.IgnoreNotFound(err)
	}

	if err := r.Delete(ctx, &ownSecret); err != nil {
		return client.IgnoreNotFound(err)
	}

	return nil
}

// Helper functions to check and remove string from a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretMirrorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1.Secret{}, secretOwnerKey, func(rawObj client.Object) []string {
		// grab the secret object, extract the owner...
		secret := rawObj.(*v1.Secret)
		owner := metav1.GetControllerOf(secret)
		if owner == nil {
			return nil
		}
		// ...make sure it's a SecretMirror...
		if owner.APIVersion != apiGVStr || owner.Kind != "SecretMirror" {
			return nil
		}

		// ...and if so, return it
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&mirrorsv1alpha1.SecretMirror{}).
		Owns(&v1.Secret{}).
		Complete(r)
}
