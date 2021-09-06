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
	"context"
	mirrorsv1alpha1 "github.com/ktsstudio/mirrors/api/v1alpha1"
	"github.com/ktsstudio/mirrors/pkg/backend"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	secretOwnerKey = ".metadata.controller"
	apiGVStr       = mirrorsv1alpha1.GroupVersion.String()
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
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;watch;list

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
	b, err := backend.MakeSecretMirrorBackend(ctx, r.Client, req.NamespacedName)
	if err != nil || b == nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, err
	}

	if err := b.Init(ctx); err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	stopReconcile, err := b.SetupOrRunFinalizer(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	if stopReconcile {
		return ctrl.Result{}, nil
	}

	if b.MirrorStatus() == "" {
		if err := b.SetStatusPending(ctx); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := b.Sync(ctx); err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	return ctrl.Result{
		RequeueAfter: b.PollPeriodDuration(),
	}, nil
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
