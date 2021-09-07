package backend

import (
	"errors"
	"fmt"
)

var (
	managedByMirrorAnnotation = "mirrors.kts.studio/owned-by"
	errNotManagedByMirror     = errors.New("resource is not managed by the Mirror")
	mirrorsFinalizerName      = "mirrors.kts.studio/finalizer"
)

const (
	DefaultWorkerPoolSize = 100
)

func getManagedByMirrorValue(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}
