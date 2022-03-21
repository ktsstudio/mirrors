package controllers

import (
	"context"
	"github.com/ktsstudio/mirrors/pkg/backend"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime"
)

type SecretMirrorBackend interface {
	SetupWithManager(mgr controllerruntime.Manager) (*controllerruntime.Builder, error)
	Init(ctx context.Context, name types.NamespacedName) (*backend.SecretMirrorContext, error)
	Cleanup()
}
