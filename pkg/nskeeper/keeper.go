package nskeeper

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"regexp"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sync"
	"time"
)

type mirrorRegex struct {
	Name    types.NamespacedName
	Regexps []*regexp.Regexp
}

type NSKeeper struct {
	client.Client
	pairs           map[string]map[string]*mirrorRegex // namespace -> name -> *mirrorRegex
	namespaces      map[string]struct{}
	pairsMutex      sync.RWMutex
	namespacesMutex sync.RWMutex
}

func (k *NSKeeper) retrieveNamespaces(ctx context.Context) (*v1.NamespaceList, error) {
	namespaces := &v1.NamespaceList{}
	if err := k.List(ctx, namespaces); err != nil {
		return nil, err
	}

	return namespaces, nil
}

func (k *NSKeeper) RegisterNamespaceRegex(mirror types.NamespacedName, regexps []*regexp.Regexp) {
	k.pairsMutex.Lock()
	defer k.pairsMutex.Unlock()

	if k.pairs == nil {
		k.pairs = make(map[string]map[string]*mirrorRegex)
	}
	if _, ok := k.pairs[mirror.Namespace]; !ok {
		k.pairs[mirror.Namespace] = make(map[string]*mirrorRegex)
	}

	k.pairs[mirror.Namespace][mirror.Name] = &mirrorRegex{
		Name:    mirror,
		Regexps: regexps,
	}
}

func (k *NSKeeper) DeregisterNamespaceRegex(mirror types.NamespacedName) {
	k.pairsMutex.Lock()
	defer k.pairsMutex.Unlock()

	if k.pairs == nil {
		return
	}
	if _, ok := k.pairs[mirror.Namespace]; !ok {
		return
	}
	if _, ok := k.pairs[mirror.Namespace][mirror.Name]; !ok {
		return
	}

	delete(k.pairs[mirror.Namespace], mirror.Name)
}

func (k *NSKeeper) AddNamespace(ns string) {
	k.namespacesMutex.Lock()
	defer k.namespacesMutex.Unlock()
	k.addNamespace(ns)
}

func (k *NSKeeper) addNamespace(ns string) {
	if _, ok := k.namespaces[ns]; ok {
		return
	}
	if k.namespaces == nil {
		k.namespaces = make(map[string]struct{})
	}
	k.namespaces[ns] = struct{}{}
}

func (k *NSKeeper) DeleteNamespace(ns string) {
	k.namespacesMutex.Lock()
	defer k.namespacesMutex.Unlock()
	delete(k.namespaces, ns)
}

func (k *NSKeeper) FindMatchingMirrors(ns string) []types.NamespacedName {
	k.pairsMutex.RLock()
	defer k.pairsMutex.RUnlock()

	if k.pairs == nil {
		return nil
	}

	var result []types.NamespacedName
	for _, mirrors := range k.pairs {
		for _, pair := range mirrors {
			for _, regex := range pair.Regexps {
				if regex.MatchString(ns) {
					result = append(result, pair.Name)
				}
			}
		}
	}
	return result
}

func (k *NSKeeper) FindMatchingNamespaces(mirror types.NamespacedName) []string {
	k.pairsMutex.RLock()
	defer k.pairsMutex.RUnlock()
	k.namespacesMutex.RLock()
	defer k.namespacesMutex.RUnlock()

	if k.pairs == nil {
		return nil
	}

	pair := k.pairs[mirror.Namespace][mirror.Name]
	if pair == nil {
		return nil
	}

	var result []string
	for ns := range k.namespaces {
		for _, regex := range pair.Regexps {
			if regex.MatchString(ns) {
				result = append(result, ns)
			}
		}
	}
	return result
}

func (k *NSKeeper) InitNamespaces(ctx context.Context) {
	logger := log.FromContext(ctx)
	k.namespacesMutex.Lock()
	defer k.namespacesMutex.Unlock()

loop:
	for {
		select {
		case <-ctx.Done():
			return
		default:
			namespaces, err := k.retrieveNamespaces(ctx)
			if err != nil {
				logger.Info(fmt.Sprintf("nskeeper: error initializing namespaces: %s", err))
				time.Sleep(3 * time.Second)
				goto loop
			}

			for _, ns := range namespaces.Items {
				k.addNamespace(ns.Name)
			}
			logger.Info(fmt.Sprintf("nskeeper: initialized with %d namespaces", len(k.namespaces)))
			return
		}
	}
}
