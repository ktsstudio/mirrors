package backend

import (
	"context"
	"fmt"
	mirrorsv1alpha2 "github.com/ktsstudio/mirrors/api/v1alpha2"
	"github.com/ktsstudio/mirrors/pkg/reconresult"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type VaultSecretDest struct {
	client.Client
	record.EventRecorder
	mirror *mirrorsv1alpha2.SecretMirror
	vault  VaultBackend
}

func (d *VaultSecretDest) Sync(ctx context.Context, secret *v1.Secret) error {
	logger := log.FromContext(ctx)

	if len(secret.Data) == 0 {
		return reconresult.Fmt("no data in source secret")
	}

	path := d.mirror.Spec.Destination.Vault.Path

	vaultSecret, err := d.vault.ReadSecret(path)
	if err != nil {
		return err
	}
	if vaultSecret != nil {
		vaultData, err := extractVaultSecretData(vaultSecret)
		if err != nil {
			return err
		}

		if !dataDiffer(secret.Data, vaultData) {
			logger.Info(fmt.Sprintf("secrets %s/%s and <vault>/%s are identical",
				secret.Namespace, secret.Name, path))
			return nil
		}
	}

	if err := d.vault.WriteData(path, map[string]interface{}{
		"data": secret.Data,
	}); err != nil {
		return &reconresult.ReconcileResult{
			Message:     fmt.Sprintf("Error syncing to vault: %s", err),
			Status:      mirrorsv1alpha2.MirrorStatusError,
			EventType:   v1.EventTypeWarning,
			EventReason: "VaultError",
		}
	}

	logger.Info("successfully synced secret to vault")
	return nil
}

func (d *VaultSecretDest) Cleanup(ctx context.Context) error {
	_ = ctx
	return nil
}
