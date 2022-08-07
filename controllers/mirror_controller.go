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
	"github.com/ktsstudio/mirrors/api/v1alpha2"
	"github.com/ktsstudio/mirrors/pkg/backend"
	"github.com/ktsstudio/mirrors/pkg/nskeeper"
	"github.com/ktsstudio/mirrors/pkg/reconresult"
	"github.com/ktsstudio/mirrors/pkg/vaulter"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"time"
)

// MirrorReconciler reconciles a SecretMirror object
type MirrorReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Backend  SecretMirrorBackend
}

//+kubebuilder:rbac:groups=mirrors.kts.studio,resources=secretmirrors,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mirrors.kts.studio,resources=secretmirrors/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mirrors.kts.studio,resources=secretmirrors/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;watch;create;update;patch;delete;list
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

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
	mirrorContext, err := r.Backend.Init(ctx, req.NamespacedName)
	if err != nil {
		return ctrl.Result{}, err
	}
	if mirrorContext == nil {
		return ctrl.Result{}, nil
	}

	return r.handleReconcileResult(ctx, mirrorContext, mirrorContext.Sync(ctx))
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

func (r *MirrorReconciler) handleReconcileResult(ctx context.Context, mirrorCtx *backend.SecretMirrorContext, err error) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var status v1alpha2.MirrorStatus
	var requeueAfter time.Duration
	if err != nil {
		if res, ok := err.(*reconresult.ReconcileResult); ok {
			logger.Info(res.Message)

			status = res.Status
			requeueAfter = res.RequeueAfter
			if requeueAfter == 0 {
				requeueAfter = mirrorCtx.SecretMirror.PollPeriodDuration()
			}

			if res.EventType != "" && res.EventReason != "" {
				r.Recorder.Event(mirrorCtx.SecretMirror, res.EventType, res.EventReason, res.Message)
			}
		} else {
			requeueAfter = reconresult.DefaultRequeueAfter
			status = v1alpha2.MirrorStatusError
		}
	} else {
		status = v1alpha2.MirrorStatusActive
		if mirrorCtx.SecretMirror.Status.MirrorStatus != v1alpha2.MirrorStatusActive {
			r.Recorder.Event(mirrorCtx.SecretMirror, v1.EventTypeNormal, "Active", "SecretMirror is synced")
		}
		requeueAfter = mirrorCtx.SecretMirror.PollPeriodDuration()
	}

	if status != "" {
		if err := mirrorCtx.SetStatus(ctx, status); err != nil {
			logger.Error(err, fmt.Sprintf("Error setting status to %s", status))
		}
	}

	return ctrl.Result{
		RequeueAfter: requeueAfter,
	}, nil
}

func SetupMirrorsReconciler(mgr ctrl.Manager, nsKeeper *nskeeper.NSKeeper) (*MirrorReconciler, error) {
	secretMirrorBackend, err := backend.MakeSecretMirrorBackend(
		mgr.GetClient(),
		mgr.GetEventRecorderFor("mirrors.kts.studio"),
		nsKeeper,
		func(addr string) (backend.VaultBackend, error) {
			return vaulter.New(addr)
		},
	)

	if err != nil {
		return nil, err
	}

	return &MirrorReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Backend:  secretMirrorBackend,
		Recorder: secretMirrorBackend.Recorder,
	}, nil
}
