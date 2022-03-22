package backend

import (
	"github.com/ktsstudio/mirrors/api/v1alpha2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sync"
)

type mirrorVaultInfo struct {
	secret          types.NamespacedName
	addr            string
	authType        v1alpha2.VaultAuthType
	appRolePath     string
	appRoleId       string
	appRoleSecretId string
	token           string
	backend         VaultBackend
}

type mirrorVaultEntry struct {
	mirror types.NamespacedName
	vaults []*mirrorVaultInfo
}

func (e *mirrorVaultEntry) findMirrorVaultInfo(secret types.NamespacedName) (int, *mirrorVaultInfo) {
	for idx, vault := range e.vaults {
		if vault.secret.Namespace == secret.Namespace && vault.secret.Name == secret.Name {
			return idx, vault
		}
	}
	return -1, nil
}

type VaultBackendMakerFunc func(addr string) (VaultBackend, error)
type MultiVault struct {
	vaults            []*mirrorVaultEntry
	vaultBackendMaker VaultBackendMakerFunc
	mutex             sync.Mutex
}

func NewMultiVault(vaultBackendMaker VaultBackendMakerFunc) *MultiVault {
	return &MultiVault{
		vaultBackendMaker: vaultBackendMaker,
	}
}

func (m *MultiVault) findMirrorVaultEntry(mirror *v1alpha2.SecretMirror) (int, *mirrorVaultEntry) {
	for idx, v := range m.vaults {
		if v.mirror.Namespace == mirror.Namespace && v.mirror.Name == mirror.Name {
			return idx, v
		}
	}
	return -1, nil
}

func (m *MultiVault) ensureMirrorVaultEntry(mirror *v1alpha2.SecretMirror) (int, *mirrorVaultEntry) {
	entryIdx, entry := m.findMirrorVaultEntry(mirror)
	if entry == nil {
		entry = &mirrorVaultEntry{
			mirror: types.NamespacedName{
				Namespace: mirror.Namespace,
				Name:      mirror.Name,
			},
		}
		m.vaults = append(m.vaults, entry)
		entryIdx = len(m.vaults) - 1
	}
	return entryIdx, entry
}

func (m *MultiVault) EnsureAndLogin(mirror *v1alpha2.SecretMirror, info mirrorVaultInfo) (VaultBackend, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.ensureAndLogin(mirror, info)
}

func (m *MultiVault) ensureAndLogin(mirror *v1alpha2.SecretMirror, info mirrorVaultInfo) (VaultBackend, error) {
	entryIdx, entry := m.ensureMirrorVaultEntry(mirror)
	vault := m.findVaultBackend(entryIdx, entry, mirror, info)

	if vault == nil {
		backend, err := m.vaultBackendMaker(info.addr)
		if err != nil {
			return nil, err
		}
		info.backend = backend
		vault = backend
	}

	if info.authType == v1alpha2.VaultAuthTypeToken {
		vault.SetToken(info.token)
	} else if info.authType == v1alpha2.VaultAuthTypeAppRole {
		if err := vault.LoginAppRole(info.appRolePath, info.appRoleId, info.appRoleSecretId); err != nil {
			return nil, err
		}
	}

	// if everything is ok - then register this vault backend
	entry.vaults = append(entry.vaults, &info)

	return vault, nil
}

func (m *MultiVault) findVaultBackend(entryIdx int, entry *mirrorVaultEntry, mirror *v1alpha2.SecretMirror, info mirrorVaultInfo) VaultBackend {
	dropIdx := -1
	var outVault VaultBackend

	idx, vaultInfo := entry.findMirrorVaultInfo(info.secret)
	if vaultInfo != nil {
		// found vault for a specified secret
		if vaultInfo.authType != info.authType ||
			vaultInfo.appRoleId != info.appRoleId ||
			vaultInfo.appRoleSecretId != info.appRoleSecretId ||
			vaultInfo.appRolePath != info.appRolePath ||
			vaultInfo.token != info.token ||
			vaultInfo.addr != info.addr {

			dropIdx = idx
		} else {
			outVault = vaultInfo.backend
		}
	}

	if dropIdx >= 0 && dropIdx < len(entry.vaults) {
		// obsolete entry - delete
		// it became obsolete because data inside its secret has been changed
		entry.vaults = append(entry.vaults[:dropIdx], entry.vaults[dropIdx+1:]...)
	}

	if len(entry.vaults) == 0 {
		m.vaults = append(m.vaults[:entryIdx], m.vaults[entryIdx+1:]...)
	}

	// need to clean up unknown secrets
	var secretRefs []v1.SecretReference
	if mirror.Spec.Source.Type == v1alpha2.SourceTypeVault {
		if mirror.Spec.Source.Vault.AuthType() == v1alpha2.VaultAuthTypeToken {
			secretRefs = append(secretRefs, mirror.Spec.Source.Vault.Auth.Token.SecretRef)
		} else if mirror.Spec.Source.Vault.AuthType() == v1alpha2.VaultAuthTypeAppRole {
			secretRefs = append(secretRefs, mirror.Spec.Source.Vault.Auth.AppRole.SecretRef)
		}
	}
	if mirror.Spec.Destination.Type == v1alpha2.DestTypeVault {
		if mirror.Spec.Destination.Vault.AuthType() == v1alpha2.VaultAuthTypeToken {
			secretRefs = append(secretRefs, mirror.Spec.Destination.Vault.Auth.Token.SecretRef)
		} else if mirror.Spec.Destination.Vault.AuthType() == v1alpha2.VaultAuthTypeAppRole {
			secretRefs = append(secretRefs, mirror.Spec.Destination.Vault.Auth.AppRole.SecretRef)
		}
	}

	stop := false
	for len(entry.vaults) > 0 && !stop {
	loop:
		for idx, vault := range entry.vaults {
			found := false
			for _, ref := range secretRefs {
				if vault.secret.Namespace == ref.Namespace && vault.secret.Name == ref.Name {
					found = true
					break
				}
			}
			if !found {
				entry.vaults = append(entry.vaults[:idx], entry.vaults[idx+1:]...)
				goto loop
			}
		}

		stop = true
	}

	return outVault
}
