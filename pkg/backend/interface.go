package backend

import (
	"context"
	mirrorsv1alpha1 "github.com/ktsstudio/mirrors/api/v1alpha1"
	"github.com/panjf2000/ants/v2"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"time"
)

type MirrorContext interface {
	ObjectName() string
	MirrorStatus() mirrorsv1alpha1.MirrorStatus
	PollPeriodDuration() time.Duration

	Init(ctx context.Context, name types.NamespacedName) error
	SyncOne(ctx context.Context, dest types.NamespacedName) error
	GetDestinationNamespaces() ([]string, error)
	SetupOrRunFinalizer(ctx context.Context) (bool, error)
	SetStatusPending(ctx context.Context) error
	SetStatus(ctx context.Context, status mirrorsv1alpha1.MirrorStatus) error
}

type MirrorBackend interface {
	Pool() *ants.Pool
	SetupWithManager(mgr ctrl.Manager) (*ctrl.Builder, error)
	Init(ctx context.Context, name types.NamespacedName) (MirrorContext, error)
	Cleanup()
}
