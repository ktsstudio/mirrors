package backend

import (
	"context"
	"fmt"
	mirrorsv1alpha2 "github.com/ktsstudio/mirrors/api/v1alpha2"
	"github.com/ktsstudio/mirrors/pkg/reconresult"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

type KubernetesSecretSource struct {
	client.Client
	Name types.NamespacedName
}

func (s *KubernetesSecretSource) Retrieve(ctx context.Context) (*v1.Secret, error) {
	var sourceSecret v1.Secret
	if err := s.Get(ctx, s.Name, &sourceSecret); err != nil {
		return nil, &reconresult.ReconcileResult{
			Message:      fmt.Sprintf("secret %s not found, waiting to appear", s.Name),
			RequeueAfter: 30 * time.Second,
			Status:       mirrorsv1alpha2.MirrorStatusPending,
			EventType:    v1.EventTypeWarning,
			EventReason:  "NoSecret",
		}
	}

	return &sourceSecret, nil
}
