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

type VaultAppRoleAuthSpec struct {
	// +optional
	SecretRef   v1.SecretReference `json:"secretRef,omitempty"`
	AppRolePath string             `json:"appRolePath,omitempty"`
	RoleIDKey   string             `json:"roleIDKey,omitempty"`
	SecretIDKey string             `json:"secretIDKey,omitempty"`
}

type VaultTokenAuthSpec struct {
	// +optional
	SecretRef v1.SecretReference `json:"secretRef,omitempty"`
	// +optional
	TokenKey string `json:"tokenKey,omitempty"`
}

type VaultAuthSpec struct {
	// +optional
	AppRole *VaultAppRoleAuthSpec `json:"approle,omitempty"`
	// +optional
	Token *VaultTokenAuthSpec `json:"token,omitempty"`
}

type VaultSpec struct {
	Addr string `json:"addr,omitempty"`
	Path string `json:"path,omitempty"`
	// +optional
	Auth VaultAuthSpec `json:"auth,omitempty"`
}

func (s *VaultSpec) Default() {
	if s.AuthType() == VaultAuthTypeAppRole {
		if s.Auth.AppRole.AppRolePath == "" {
			s.Auth.AppRole.AppRolePath = "approle"
		}
		if s.Auth.AppRole.RoleIDKey == "" {
			s.Auth.AppRole.RoleIDKey = "role-id"
		}
		if s.Auth.AppRole.SecretIDKey == "" {
			s.Auth.AppRole.SecretIDKey = "secret-id"
		}
	} else if s.AuthType() == VaultAuthTypeToken {
		if s.Auth.Token.TokenKey == "" {
			s.Auth.Token.TokenKey = "token"
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

	if s.AuthType() == VaultAuthTypeAppRole {
		if s.Auth.AppRole.SecretRef.Name == "" {
			return errors.New("vault.auth.appRole.secretRef.name is required when using appRole auth")
		}

	} else if s.AuthType() == VaultAuthTypeToken {
		if s.Auth.Token.SecretRef.Name == "" {
			return errors.New("vault.auth.token.secretRef.name is required when using token auth")
		}
	}

	return nil
}

func (s *VaultSpec) AuthType() VaultAuthType {
	if s.Auth.AppRole.SecretRef.Name != "" {
		return VaultAuthTypeAppRole
	}

	return VaultAuthTypeToken
}
