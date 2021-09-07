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
	"github.com/ktsstudio/mirrors/pkg/backend"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sync"
)

// MirrorReconciler reconciles a SecretMirror object
type MirrorReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Backend backend.MirrorBackend
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
func (r *MirrorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	mirrorContext, err := r.Backend.Init(ctx, req.NamespacedName)
	if err != nil || mirrorContext == nil {
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
		if err := mirrorContext.SetStatusPending(ctx); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.Sync(ctx, mirrorContext); err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	return ctrl.Result{
		RequeueAfter: mirrorContext.PollPeriodDuration(),
	}, nil
}

func (r *MirrorReconciler) Sync(ctx context.Context, mirrorContext backend.MirrorContext) error {
	logger := log.FromContext(ctx)

	namespaces, err := mirrorContext.GetDestinationNamespaces()
	if err != nil {
		return err
	}
	wg := &sync.WaitGroup{}
	g := &errgroup.Group{}
	for _, ns := range namespaces {
		ns := ns
		wg.Add(1)

		if err := r.Backend.Pool().Submit(func() {
			g.Go(func() error {
				defer wg.Done()
				return mirrorContext.SyncOne(ctx, types.NamespacedName{
					Namespace: ns,
					Name:      mirrorContext.ObjectName(),
				})
			})
		}); err != nil {
			return err
		}
	}
	wg.Wait()

	if err := g.Wait(); err != nil {
		logger.Error(err, fmt.Sprintf("unable to sync some secrets for %s", mirrorContext))
		_ = mirrorContext.SetStatus(ctx, mirrorsv1alpha1.MirrorStatusError)
		return err
	}

	if err := mirrorContext.SetStatus(ctx, mirrorsv1alpha1.MirrorStatusActive); err != nil {
		logger.Error(err, "unable to update SecretMirror status")
		return err
	}
	return nil
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
