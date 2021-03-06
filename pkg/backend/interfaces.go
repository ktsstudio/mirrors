package backend

import (
	"context"
	v1 "k8s.io/api/core/v1"
)

type SourceRetriever interface {
	Setup(ctx context.Context) error
	Retrieve(ctx context.Context) (*v1.Secret, error)
}

type DestSyncer interface {
	Setup(ctx context.Context) error
	Sync(ctx context.Context, secret *v1.Secret) error
	Cleanup(ctx context.Context) error
}
