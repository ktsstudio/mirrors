package backend

import (
	"context"
	mirrorsv1alpha1 "github.com/ktsstudio/mirrors/api/v1alpha1"
	"time"
)

type MirrorBackend interface {
	MirrorStatus() mirrorsv1alpha1.MirrorStatus
	PollPeriodDuration() time.Duration

	Init(ctx context.Context) error
	Sync(ctx context.Context) error
	SetupOrRunFinalizer(ctx context.Context) (bool, error)
	SetStatusPending(ctx context.Context) error
	SetStatus(ctx context.Context, status mirrorsv1alpha1.MirrorStatus) error
}
