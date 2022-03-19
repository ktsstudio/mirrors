package backend

import (
	"context"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime"
)

type MirrorBackend interface {
	SetupWithManager(mgr controllerruntime.Manager) (*controllerruntime.Builder, error)
	Init(ctx context.Context, name types.NamespacedName) (MirrorContext, error)
	Cleanup()
}
