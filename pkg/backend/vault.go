package backend

import (
	"context"
	"encoding/base64"
	"fmt"
	vault "github.com/hashicorp/vault/api"
	mirrorsv1alpha2 "github.com/ktsstudio/mirrors/api/v1alpha2"
	"github.com/ktsstudio/mirrors/pkg/reconresult"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type VaultBackend interface {
	Addr() string
	Token() string
	SetToken(token string)
	LoginAppRole(appRolePath, roleID, secretID string) error
	ReadSecret(path string) (*vault.Secret, error)
	RetrieveData(path string) (map[string]interface{}, error)
	WriteData(path string, data map[string]interface{}) error
	RenewLease(leaseId string, increment int) (*vault.Secret, error)
}

func authVaultBackend(ctx context.Context, cli client.Client, vault VaultBackend, auth *mirrorsv1alpha2.VaultAuthSpec) error {
	logger := log.FromContext(ctx)

	if auth.Type() == mirrorsv1alpha2.VaultAuthTypeToken {
		tokenSecretName := types.NamespacedName{
			Name:      auth.Token.SecretRef.Name,
			Namespace: auth.Token.SecretRef.Namespace,
		}
		tokenSecret, err := fetchSecret(ctx, cli, tokenSecretName)
		if err != nil {
			return err
		}

		if tokenSecret == nil {
			return &reconresult.ReconcileResult{
				Message:     fmt.Sprintf("secret %s for vault token not found", tokenSecretName),
				Status:      mirrorsv1alpha2.MirrorStatusPending,
				EventType:   v1.EventTypeWarning,
				EventReason: "VaultAuthMissing",
			}
		}

		token, exists := tokenSecret.Data[auth.Token.TokenKey]
		if !exists {
			return &reconresult.ReconcileResult{
				Message:     fmt.Sprintf("cannot find token under secret %s and key %s", tokenSecretName, auth.Token.TokenKey),
				Status:      mirrorsv1alpha2.MirrorStatusPending,
				EventType:   v1.EventTypeWarning,
				EventReason: "VaultAuthMissing",
			}
		}

		vault.SetToken(string(token))

	} else if auth.Type() == mirrorsv1alpha2.VaultAuthTypeAppRole {
		appRoleSecretName := types.NamespacedName{
			Name:      auth.AppRole.SecretRef.Name,
			Namespace: auth.AppRole.SecretRef.Namespace,
		}
		appRoleSecret, err := fetchSecret(ctx, cli, appRoleSecretName)
		if err != nil {
			return err
		}

		if appRoleSecret == nil {
			return &reconresult.ReconcileResult{
				Message:     fmt.Sprintf("secret %s for vault approle login not found", appRoleSecretName),
				Status:      mirrorsv1alpha2.MirrorStatusPending,
				EventType:   v1.EventTypeWarning,
				EventReason: "VaultAuthMissing",
			}
		}

		roleID, exists := appRoleSecret.Data[auth.AppRole.RoleIDKey]
		if !exists {
			return &reconresult.ReconcileResult{
				Message:     fmt.Sprintf("cannot find roleID under secret %s and key %s", appRoleSecretName, auth.AppRole.RoleIDKey),
				Status:      mirrorsv1alpha2.MirrorStatusPending,
				EventType:   v1.EventTypeWarning,
				EventReason: "VaultAuthMissing",
			}
		}
		secretID, exists := appRoleSecret.Data[auth.AppRole.SecretIDKey]
		if !exists {
			return &reconresult.ReconcileResult{
				Message:     fmt.Sprintf("cannot find secretID under secret %s and key %s", appRoleSecretName, auth.AppRole.SecretIDKey),
				Status:      mirrorsv1alpha2.MirrorStatusPending,
				EventType:   v1.EventTypeWarning,
				EventReason: "VaultAuthMissing",
			}
		}
		if err := vault.LoginAppRole(auth.AppRole.AppRolePath, string(roleID), string(secretID)); err != nil {
			return &reconresult.ReconcileResult{
				Message:     fmt.Sprintf("error logging in to vault via approle: %s", err),
				Status:      mirrorsv1alpha2.MirrorStatusError,
				EventType:   v1.EventTypeWarning,
				EventReason: "VaultAuthInvalid",
			}
		}
	}

	logger.Info("successfully logged in to vault")

	return nil
}

func extractVaultSecretData(secret *vault.Secret) (map[string][]byte, error) {
	var vaultData map[string]interface{}

	if data, ok := secret.Data["data"]; ok {
		if data, ok := data.(map[string]interface{}); ok {
			vaultData = data
		}
	} else {
		vaultData = secret.Data
	}

	data := make(map[string][]byte, len(vaultData))
	for k, v := range vaultData {
		stringValue, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("vault key %s contains non-string value", k)
		}
		var value []byte
		// try to decode base64 strings
		decodedValue, err := base64.StdEncoding.DecodeString(stringValue)
		if err == nil {
			// indeed a base64 string
			value = decodedValue
		} else {
			value = []byte(stringValue)
		}
		data[k] = value
	}
	return data, nil
}
