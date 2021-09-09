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
	"fmt"
	mirrorsv1alpha1 "github.com/ktsstudio/mirrors/api/v1alpha1"
	"github.com/ktsstudio/mirrors/pkg/nskeeper"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// NamespaceReconciler reconciles a Namespace object
type NamespaceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	nsKeeper *nskeeper.NSKeeper
}

//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;watch;list

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Namespace object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var ns v1.Namespace
	if err := r.Get(ctx, req.NamespacedName, &ns); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// examine DeletionTimestamp to determine if object is under deletion
	if ns.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		logger.Info(fmt.Sprintf("new namespace: %s", ns.Name))
		r.nsKeeper.AddNamespace(ns.Name)
	} else {
		// The object is being deleted
		logger.Info(fmt.Sprintf("namespace deleted: %s", ns.Name))
		r.nsKeeper.DeleteNamespace(ns.Name)

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	mirrors := r.nsKeeper.FindMatchingMirrors(ns.Name)
	for _, mirror := range mirrors {
		logger.Info(fmt.Sprintf("triggering mirror reconcile for %s", mirror))

		if err := r.triggerSecretMirrorReconcile(ctx, mirror); err != nil {
			logger.Error(err, fmt.Sprintf("error triggering mirror reconcile for %s", mirror))
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager, nsKeeper *nskeeper.NSKeeper) error {
	r.nsKeeper = nsKeeper

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Namespace{}).
		Complete(r)
}

func (r *NamespaceReconciler) triggerSecretMirrorReconcile(ctx context.Context, name types.NamespacedName) error {
	var secretMirror mirrorsv1alpha1.SecretMirror
	if err := r.Get(ctx, name, &secretMirror); err != nil {
		return client.IgnoreNotFound(err)
	}

	secretMirror.Status.LastSyncTime = metav1.Now()
	if err := r.Status().Update(ctx, &secretMirror); err != nil {
		return err
	}
	return nil
}
