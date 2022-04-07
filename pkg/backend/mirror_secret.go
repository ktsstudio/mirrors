package backend

import (
	"context"
	"fmt"
	mirrorsv1alpha2 "github.com/ktsstudio/mirrors/api/v1alpha2"
	"github.com/ktsstudio/mirrors/pkg/metrics"
	"github.com/ktsstudio/mirrors/pkg/nskeeper"
	"github.com/ktsstudio/mirrors/pkg/reconresult"
	"github.com/panjf2000/ants/v2"
	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"time"
)

type SecretMirrorContext struct {
	backend *SecretMirrorBackend

	SecretMirror *mirrorsv1alpha2.SecretMirror
}

func (c *SecretMirrorContext) Init(ctx context.Context, name types.NamespacedName) error {
	logger := log.FromContext(ctx)
	logger.Info(fmt.Sprintf("reconciling secret mirror %s", name))

	var secretMirror mirrorsv1alpha2.SecretMirror
	if err := c.backend.Client.Get(ctx, name, &secretMirror); err != nil {
		return client.IgnoreNotFound(err)
	}

	c.SecretMirror = &secretMirror
	c.SecretMirror.Default()

	return nil
}

// SetupOrRunFinalizer returns (stopReconciliation, error)
func (c *SecretMirrorContext) SetupOrRunFinalizer(ctx context.Context) (bool, error) {
	logger := log.FromContext(ctx)

	// examine DeletionTimestamp to determine if object is under deletion
	if c.SecretMirror.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !containsString(c.SecretMirror.GetFinalizers(), mirrorsFinalizerName) {
			controllerutil.AddFinalizer(c.SecretMirror, mirrorsFinalizerName)
			if err := c.backend.Update(ctx, c.SecretMirror); err != nil {
				return false, err
			}
		}
	} else {
		// The object is being deleted
		if containsString(c.SecretMirror.GetFinalizers(), mirrorsFinalizerName) {
			// our finalizer is present, so lets handle any external dependency
			syncer, err := c.makeDestSyncer(ctx)
			if err != nil {
				return false, err
			}
			if err := syncer.Cleanup(ctx); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return false, err
			}
			if c.SecretMirror.Spec.DeletePolicy == mirrorsv1alpha2.DeletePolicyDelete {
				logger.Info("deleted managed objects")
			} else {
				logger.Info("retaining all managed secrets")
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(c.SecretMirror, mirrorsFinalizerName)
			if err := c.backend.Update(ctx, c.SecretMirror); err != nil {
				return false, err
			}
		}

		// Stop reconciliation as the item is being deleted
		return true, nil
	}

	return false, nil
}

func (c *SecretMirrorContext) SetStatus(ctx context.Context, status mirrorsv1alpha2.MirrorStatus) error {
	logger := log.FromContext(ctx)
	logger.Info("setting status", "status", status, "was", c.SecretMirror.Status.MirrorStatus)
	c.SecretMirror.Status.MirrorStatus = status
	if status == mirrorsv1alpha2.MirrorStatusPending {
		if c.SecretMirror.Status.LastSyncTime.IsZero() {
			c.SecretMirror.Status.LastSyncTime = metav1.Unix(0, 0)
		}
	} else {
		c.SecretMirror.Status.LastSyncTime = metav1.Now()
	}
	return c.backend.Status().Update(ctx, c.SecretMirror)
}

func (c *SecretMirrorContext) Sync(ctx context.Context) error {
	if c.SecretMirror.Status.MirrorStatus == "" {
		if err := c.SetStatus(ctx, mirrorsv1alpha2.MirrorStatusPending); err != nil {
			return err
		}
	}

	sourceRetriever, err := c.makeSourceRetriever(ctx)
	if err != nil {
		return err
	}
	if err := sourceRetriever.Setup(ctx); err != nil {
		return err
	}

	destSyncer, err := c.makeDestSyncer(ctx)
	if err != nil {
		return err
	}

	if err := destSyncer.Setup(ctx); err != nil {
		return err
	}

	// only check after we have set up everything (e.g. registered namespaces in nsKeeper)
	now := time.Now()
	nextSyncAt := c.SecretMirror.Status.LastSyncTime.Time.Add(c.SecretMirror.PollPeriodDuration())
	if now.Before(nextSyncAt) {
		return &reconresult.ReconcileResult{
			Message:      fmt.Sprintf("no need to sync. next sync at %s", nextSyncAt),
			RequeueAfter: nextSyncAt.Sub(now),
		}
	}

	sourceSecret, err := sourceRetriever.Retrieve(ctx)
	if err != nil {
		return err
	}

	if err := destSyncer.Sync(ctx, sourceSecret); err != nil {
		return err
	}

	metrics.MirrorSyncCount.With(prometheus.Labels{
		"mirror":           getPrettyName(c.SecretMirror),
		"source_type":      string(c.SecretMirror.Spec.Source.Type),
		"destination_type": string(c.SecretMirror.Spec.Destination.Type),
	}).Inc()

	return nil
}

func (c *SecretMirrorContext) makeSourceRetriever(ctx context.Context) (SourceRetriever, error) {
	if c.SecretMirror.Spec.Source.Type == mirrorsv1alpha2.SourceTypeSecret {
		return &KubernetesSecretSource{
			Client: c.backend.Client,
			Name: types.NamespacedName{
				Namespace: c.SecretMirror.Namespace,
				Name:      c.SecretMirror.Spec.Source.Name,
			},
		}, nil

	} else if c.SecretMirror.Spec.Source.Type == mirrorsv1alpha2.SourceTypeVault {
		vault, err := c.backend.makeVault(ctx, c.SecretMirror.Spec.Source.Vault)
		if err != nil {
			return nil, err
		}
		return &VaultSecretSource{
			Client:        c.backend,
			EventRecorder: c.backend.Recorder,
			mirror:        c.SecretMirror,
			vault:         vault,
		}, nil

	}

	return nil, fmt.Errorf("source.type %s is unsupported", c.SecretMirror.Spec.Source.Type)
}

func (c *SecretMirrorContext) makeDestSyncer(ctx context.Context) (DestSyncer, error) {
	if c.SecretMirror.Spec.Destination.Type == mirrorsv1alpha2.DestTypeNamespaces {
		return &NamespacesDest{
			Client:        c.backend,
			EventRecorder: c.backend.Recorder,
			mirror:        c.SecretMirror,
			nsKeeper:      c.backend.nsKeeper,
			pool:          c.backend.pool,
		}, nil

	} else if c.SecretMirror.Spec.Destination.Type == mirrorsv1alpha2.DestTypeVault {
		vault, err := c.backend.makeVault(ctx, c.SecretMirror.Spec.Destination.Vault)
		if err != nil {
			return nil, err
		}

		return &VaultSecretDest{
			Client:        c.backend,
			EventRecorder: c.backend.Recorder,
			mirror:        c.SecretMirror,
			vault:         vault,
		}, nil
	}

	return nil, fmt.Errorf("unknown destination type: %s", c.SecretMirror.Spec.Destination.Type)
}

/// Backend

type VaultBackendMakerFunc func(addr string) (VaultBackend, error)
type SecretMirrorBackend struct {
	client.Client
	Recorder          record.EventRecorder
	nsKeeper          *nskeeper.NSKeeper
	pool              *ants.Pool
	vaultBackendMaker VaultBackendMakerFunc
}

func MakeSecretMirrorBackend(cli client.Client, recorder record.EventRecorder, nsKeeper *nskeeper.NSKeeper, vaultBackendMaker VaultBackendMakerFunc) (*SecretMirrorBackend, error) {
	pool, err := ants.NewPool(DefaultWorkerPoolSize)
	if err != nil {
		return nil, err
	}
	return &SecretMirrorBackend{
		Client:            cli,
		Recorder:          recorder,
		nsKeeper:          nsKeeper,
		pool:              pool,
		vaultBackendMaker: vaultBackendMaker,
	}, nil
}

func (b *SecretMirrorBackend) SetupWithManager(mgr ctrl.Manager) (*ctrl.Builder, error) {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mirrorsv1alpha2.SecretMirror{}).
		Owns(&v1.Secret{}), nil
}

func (b *SecretMirrorBackend) Init(ctx context.Context, name types.NamespacedName) (*SecretMirrorContext, error) {
	mirrorContext := &SecretMirrorContext{
		backend: b,
	}
	if err := mirrorContext.Init(ctx, name); err != nil {
		return nil, err
	}

	if mirrorContext.SecretMirror == nil {
		return nil, nil
	}

	stopReconcile, err := mirrorContext.SetupOrRunFinalizer(ctx)
	if err != nil {
		return nil, err
	}

	if stopReconcile {
		return nil, nil
	}

	return mirrorContext, nil
}

func (b *SecretMirrorBackend) Cleanup() {
	b.pool.Release()
}

func (b *SecretMirrorBackend) makeVault(ctx context.Context, spec *mirrorsv1alpha2.VaultSpec) (VaultBackend, error) {
	vault, err := b.vaultBackendMaker(spec.Addr)
	if err != nil {
		return nil, err
	}
	if err := authVaultBackend(ctx, b, vault, &spec.Auth); err != nil {
		return nil, err
	}
	return vault, nil
}
