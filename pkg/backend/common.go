package backend

import (
	"errors"
	"fmt"
)

var (
	managedByMirrorAnnotation = "mirrors.kts.studio/owned-by"
	notManagedByMirror        = errors.New("resource is not managed by the Mirror")
	mirrorsFinalizerName      = "mirrors.kts.studio/finalizer"
)

func getManagedByMirrorValue(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}
