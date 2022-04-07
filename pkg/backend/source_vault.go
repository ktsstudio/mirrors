package backend

import (
	"context"
	"fmt"
	"github.com/hashicorp/vault/api"
	mirrorsv1alpha2 "github.com/ktsstudio/mirrors/api/v1alpha2"
	"github.com/ktsstudio/mirrors/pkg/metrics"
	"github.com/ktsstudio/mirrors/pkg/reconresult"
	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type VaultSecretSource struct {
	client.Client
	record.EventRecorder
	mirror *mirrorsv1alpha2.SecretMirror
	vault  VaultBackend
}

func (s *VaultSecretSource) Setup(ctx context.Context) error {
	_ = ctx
	return nil
}

func (s *VaultSecretSource) Retrieve(ctx context.Context) (*v1.Secret, error) {
	path := s.mirror.Spec.Source.Vault.Path

	data, err := s.retrieveVaultSecret(ctx, s.vault, path)
	if err != nil {
		return nil, err
	}

	if data == nil {
		return nil, &reconresult.ReconcileResult{
			Message:      fmt.Sprintf("no data need to be synced, vaultPath: %s", path),
			RequeueAfter: s.mirror.PollPeriodDuration(),
			Status:       mirrorsv1alpha2.MirrorStatusActive,
		}
	}

	var sourceSecret v1.Secret
	sourceSecret.Data = data
	sourceSecret.Namespace = "<vault>"
	sourceSecret.Name = path

	return &sourceSecret, nil
}

func (s *VaultSecretSource) retrieveVaultSecret(ctx context.Context, vault VaultBackend, path string) (map[string][]byte, error) {
	logger := log.FromContext(ctx)

	if s.mirror.Status.VaultSource != nil && s.mirror.Status.VaultSource.LeaseID != "" {
		leaseResult, err := vault.RenewLease(s.mirror.Status.VaultSource.LeaseID, s.mirror.Status.VaultSource.LeaseDuration)
		if err != nil {
			logger.Info("error while renewing lease - will refetch secret", "err", err, "lease-id", s.mirror.Status.VaultSource.LeaseID)
			s.mirror.Status.VaultSource = nil

			statusCode := "-"
			if err, ok := err.(*api.ResponseError); ok {
				statusCode = fmt.Sprintf("%d", err.StatusCode)
			}
			metrics.VaultLeaseRenewErrorCount.With(prometheus.Labels{
				"mirror":    getPrettyName(s.mirror),
				"vault":     vault.Addr(),
				"http_code": statusCode,
			}).Inc()
		} else {
			s.Eventf(s.mirror, v1.EventTypeNormal, "VaultLeaseRenew", "Renewed lease successfully")
			metrics.VaultLeaseRenewOkCount.With(prometheus.Labels{
				"mirror": getPrettyName(s.mirror),
				"vault":  vault.Addr(),
			}).Inc()
			logger.Info("successfully renewed vault lease", "leaseId", s.mirror.Status.VaultSource.LeaseID)
			s.mirror.Status.VaultSource.LeaseID = leaseResult.LeaseID
			s.mirror.Status.VaultSource.LeaseDuration = leaseResult.LeaseDuration
		}

		if s.mirror.Status.VaultSource != nil {
			// no need to fetch data as we prolonged a lease successfully
			return nil, nil
		}
	}

	vaultSecret, err := vault.ReadSecret(path)
	if err != nil {
		return nil, err
	}
	if vaultSecret == nil {
		return nil, nil
	}

	if vaultSecret.Renewable {
		if s.mirror.Status.VaultSource == nil {
			s.mirror.Status.VaultSource = &mirrorsv1alpha2.VaultSourceStatusSpec{}
		}
		s.mirror.Status.VaultSource.LeaseID = vaultSecret.LeaseID
		s.mirror.Status.VaultSource.LeaseDuration = vaultSecret.LeaseDuration

		s.Eventf(s.mirror, v1.EventTypeNormal, "VaultNewCreds", "Fetched new credentials under the lease %s", vaultSecret.LeaseID)
	}

	return extractVaultSecretData(vaultSecret)
}
