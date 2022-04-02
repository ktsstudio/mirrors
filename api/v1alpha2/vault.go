package v1alpha2

import (
	"errors"
	"k8s.io/api/core/v1"
)

type VaultAuthType string

const (
	VaultAuthTypeAppRole VaultAuthType = "appRole"
	VaultAuthTypeToken   VaultAuthType = "token"
)

// VaultAppRoleAuthSpec specifies approle-specific auth data
type VaultAppRoleAuthSpec struct {
	// Reference to a Secret containing role-id and secret-id
	// +optional
	SecretRef v1.SecretReference `json:"secretRef,omitempty"`

	// approle Vault prefix. Default: approle
	AppRolePath string `json:"appRolePath,omitempty"`

	// A key in the SecretRef which contains role-id value. Default: role-id
	RoleIDKey string `json:"roleIDKey,omitempty"`

	// A key in the SecretRef which contains secret-id value. Default: secret-id
	SecretIDKey string `json:"secretIDKey,omitempty"`
}

// VaultTokenAuthSpec specifies token-specific auth data
type VaultTokenAuthSpec struct {
	// Reference to a Secret containing token
	// +optional
	SecretRef v1.SecretReference `json:"secretRef,omitempty"`

	// A key in the SecretRef which contains token value. Default: token
	// +optional
	TokenKey string `json:"tokenKey,omitempty"`
}

// VaultAuthSpec describes how to authenticate against a Vault server
type VaultAuthSpec struct {
	// +optional
	AppRole *VaultAppRoleAuthSpec `json:"approle,omitempty"`
	// +optional
	Token *VaultTokenAuthSpec `json:"token,omitempty"`
}

func (s *VaultAuthSpec) Type() VaultAuthType {
	if s.AppRole != nil && s.AppRole.SecretRef.Name != "" {
		return VaultAuthTypeAppRole
	}

	return VaultAuthTypeToken
}

// VaultSpec contains information of secret location
type VaultSpec struct {
	// Addr specifies a Vault endpoint URL (e.g. https://vault.example.com)
	Addr string `json:"addr,omitempty"`
	// Path specifies a vault secret path (e.g. secret/data/some-secret or mongodb/creds/mymongo)
	Path string `json:"path,omitempty"`
	// +optional
	Auth VaultAuthSpec `json:"auth,omitempty"`
}

func (s *VaultSpec) Default(namespace string) {
	if s.Auth.Type() == VaultAuthTypeAppRole {
		if s.Auth.AppRole.AppRolePath == "" {
			s.Auth.AppRole.AppRolePath = "approle"
		}
		if s.Auth.AppRole.RoleIDKey == "" {
			s.Auth.AppRole.RoleIDKey = "role-id"
		}
		if s.Auth.AppRole.SecretIDKey == "" {
			s.Auth.AppRole.SecretIDKey = "secret-id"
		}
		if s.Auth.AppRole.SecretRef.Namespace == "" {
			s.Auth.AppRole.SecretRef.Namespace = namespace
		}
	} else if s.Auth.Type() == VaultAuthTypeToken {
		if s.Auth.Token.TokenKey == "" {
			s.Auth.Token.TokenKey = "token"
		}
		if s.Auth.Token.SecretRef.Namespace == "" {
			s.Auth.Token.SecretRef.Namespace = namespace
		}
	}
}

func (s *VaultSpec) Validate() error {
	if s.Addr == "" {
		return errors.New("destination.vault.addr must be specified")
	}

	if s.Path == "" {
		return errors.New("destination.vault.path must be specified")
	}

	if s.Auth.Type() == VaultAuthTypeAppRole {
		if s.Auth.AppRole.SecretRef.Name == "" {
			return errors.New("vault.auth.appRole.secretRef.name is required when using appRole auth")
		}

	} else if s.Auth.Type() == VaultAuthTypeToken {
		if s.Auth.Token.SecretRef.Name == "" {
			return errors.New("vault.auth.token.secretRef.name is required when using token auth")
		}
	}

	return nil
}
