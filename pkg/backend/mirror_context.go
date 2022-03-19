package backend

import (
	"context"
	"github.com/ktsstudio/mirrors/api/v1alpha2"
	"k8s.io/apimachinery/pkg/types"
	"time"
)

type MirrorContext interface {
	MirrorStatus() v1alpha2.MirrorStatus
	PollPeriodDuration() time.Duration

	Init(ctx context.Context, name types.NamespacedName) error
	Sync(ctx context.Context) error
	SetupOrRunFinalizer(ctx context.Context) (bool, error)
	SetStatus(ctx context.Context, status v1alpha2.MirrorStatus) error
}
