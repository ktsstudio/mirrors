package nskeeper

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sync"
	"sync/atomic"
	"time"
)

type NSKeeper struct {
	client.Client
	Period     time.Duration
	namespaces *v1.NamespaceList
	mutex      sync.Mutex
	running    uint32
}

func (k *NSKeeper) GetNamespaces() *v1.NamespaceList {
	return k.namespaces
}

func (k *NSKeeper) IsRunning() bool {
	value := atomic.LoadUint32(&k.running)
	return value > 0
}

func (k *NSKeeper) setIsRunning(flag bool) {
	var value uint32
	if flag {
		value = 1
	} else {
		value = 0
	}
	atomic.StoreUint32(&k.running, value)
}

func (k *NSKeeper) Run(ctx context.Context) {
	if k.Period == 0 {
		k.Period = 1 * time.Minute
	}

	logger := log.FromContext(ctx)

	if k.IsRunning() {
		logger.Info("nskeeper is already running")
		return
	}

	k.setIsRunning(true)
	defer func() {
		k.setIsRunning(false)
	}()

	sleepPeriod := k.Period

	if err := k.loop(ctx); err != nil {
		logger.Info(fmt.Sprintf("Error happened in nskeeper loop: %s", err))
		sleepPeriod = 5 * time.Second
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			time.Sleep(sleepPeriod)
			if err := k.loop(ctx); err != nil {
				logger.Info(fmt.Sprintf("Error happened in nskeeper loop: %s", err))
				sleepPeriod = 5 * time.Second
			} else {
				sleepPeriod = k.Period
			}
		}
	}
}

func (k *NSKeeper) loop(ctx context.Context) error {
	logger := log.FromContext(ctx)

	k.mutex.Lock()
	defer k.mutex.Unlock()

	nsList, err := k.retrieveNamespaces(ctx)
	if err != nil {
		return err
	}

	k.namespaces = nsList
	logger.Info(fmt.Sprintf("nskeeper: refreshed namespaces list. total count: %d", len(nsList.Items)))
	return nil
}

func (k *NSKeeper) retrieveNamespaces(ctx context.Context) (*v1.NamespaceList, error) {
	namespaces := &v1.NamespaceList{}
	if err := k.List(ctx, namespaces); err != nil {
		return nil, err
	}

	return namespaces, nil
}
