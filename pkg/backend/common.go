package backend

import (
	"fmt"
)

var (
	ownedByMirrorAnnotation = "mirrors.kts.studio/owned-by"
	lastSyncAnnotation      = "mirrors.kts.studio/last-sync-at"
	parentVersionAnnotation = "mirrors.kts.studio/parent-version"
	sourceTypeAnnotation    = "mirrors.kts.studio/source-type"
	vaultPathAnnotation     = "mirrors.kts.studio/vault-path"
	mirrorsFinalizerName    = "mirrors.kts.studio/finalizer"
)

const (
	DefaultWorkerPoolSize = 100
)

func getManagedByMirrorValue(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}
