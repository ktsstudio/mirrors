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
	"github.com/ktsstudio/mirrors/api/v1alpha2"
	"github.com/ktsstudio/mirrors/pkg/silenterror"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// MirrorReconciler reconciles a SecretMirror object
type MirrorReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Backend SecretMirrorBackend
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
func (r *MirrorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	mirrorContext, err := r.Backend.Init(ctx, req.NamespacedName)
	if err != nil {
		return silenterror.ToCtrlResult(logger, err)
	} else if mirrorContext == nil {
		return ctrl.Result{}, err
	}

	stopReconcile, err := mirrorContext.SetupOrRunFinalizer(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	if stopReconcile {
		return ctrl.Result{}, nil
	}

	if mirrorContext.MirrorStatus() == "" {
		if err := mirrorContext.SetStatus(ctx, v1alpha2.MirrorStatusPending); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := mirrorContext.Sync(ctx); err != nil {
		return silenterror.ToCtrlResult(logger, err)
	}

	return ctrl.Result{
		RequeueAfter: mirrorContext.PollPeriodDuration(),
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MirrorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder, err := r.Backend.SetupWithManager(mgr)
	if err != nil {
		return err
	}
	return builder.Complete(r)
}

func (r *MirrorReconciler) Cleanup() {
	r.Backend.Cleanup()
}
