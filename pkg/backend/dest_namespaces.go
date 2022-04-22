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
	"golang.org/x/sync/errgroup"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"regexp"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sync"
)

type NamespacesDest struct {
	client.Client
	record.EventRecorder
	mirror   *mirrorsv1alpha2.SecretMirror
	nsKeeper *nskeeper.NSKeeper
	pool     *ants.Pool
}

func (d *NamespacesDest) Setup(ctx context.Context) error {
	_ = ctx
	if err := d.registerNamespaces(); err != nil {
		return err
	}
	return nil
}

func (d *NamespacesDest) Sync(ctx context.Context, secret *v1.Secret) error {
	wg := &sync.WaitGroup{}
	g := &errgroup.Group{}
	destNamespaces := d.getDestinationNamespaces()
	for _, ns := range destNamespaces {
		ns := ns
		wg.Add(1)

		if err := d.pool.Submit(func() {
			g.Go(func() error {
				defer wg.Done()
				return d.syncOneToNamespace(ctx, secret, types.NamespacedName{
					Namespace: ns,
					Name:      d.mirror.Spec.Source.Name,
				})
			})
		}); err != nil {
			return err
		}
	}
	wg.Wait()

	if err := g.Wait(); err != nil {
		return &reconresult.ReconcileResult{
			Message:     fmt.Sprintf("unable to sync some objects: %s", err),
			Status:      mirrorsv1alpha2.MirrorStatusError,
			EventType:   v1.EventTypeWarning,
			EventReason: "SyncError",
		}
	}

	metrics.MirrorNSCurrentCount.With(prometheus.Labels{
		"mirror":      getPrettyName(d.mirror),
		"source_type": string(d.mirror.Spec.Source.Type),
	}).Set(float64(len(destNamespaces)))
	return nil
}

func (d *NamespacesDest) registerNamespaces() error {
	if len(d.mirror.Spec.Destination.Namespaces) > 0 {
		regexps := make([]*regexp.Regexp, 0, len(d.mirror.Spec.Destination.Namespaces))
		for _, regexRaw := range d.mirror.Spec.Destination.Namespaces {
			regex, err := regexp.Compile(regexRaw)
			if err != nil {
				return err
			}
			regexps = append(regexps, regex)
		}

		d.nsKeeper.RegisterNamespaceRegex(types.NamespacedName{
			Namespace: d.mirror.Namespace,
			Name:      d.mirror.Name,
		}, regexps)
	}
	return nil
}

func (d *NamespacesDest) syncOneToNamespace(ctx context.Context, secret *v1.Secret, dest types.NamespacedName) error {
	logger := log.FromContext(ctx)

	destSecret, err := FetchSecret(ctx, d, dest)
	if err != nil {
		return err
	}

	if !d.validateAnnotations(ctx, destSecret) {
		return nil
	}

	doCreate := false
	if destSecret == nil {
		// secret does not exist yet
		destSecret = &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:        dest.Name,
				Namespace:   dest.Namespace,
				Labels:      make(map[string]string),
				Annotations: make(map[string]string),
			},
			Type: secret.Type,
		}
		doCreate = true
	}

	if !secretDiffer(secret, destSecret) {
		logger.Info(fmt.Sprintf("secrets %s/%s and %s/%s are identical",
			secret.Namespace, secret.Name, destSecret.Namespace, destSecret.Name))
		return nil
	}

	copySecret(secret, destSecret)
	destSecret.Annotations[ownedByMirrorAnnotation] = d.getManagedByMirrorValue()
	destSecret.Annotations[lastSyncAnnotation] = metav1.Now().String()
	destSecret.Annotations[parentVersionAnnotation] = secret.ResourceVersion
	destSecret.Annotations[sourceTypeAnnotation] = string(d.mirror.Spec.Source.Type)
	if d.mirror.Spec.Source.Type == mirrorsv1alpha2.SourceTypeVault {
		destSecret.Annotations[vaultPathAnnotation] = d.mirror.Spec.Source.Vault.Path

		if d.mirror.Status.VaultSource != nil {
			destSecret.Annotations[vaultLeaseIdAnnotation] = d.mirror.Status.VaultSource.LeaseID
			destSecret.Annotations[vaultLeaseDurationAnnotation] = fmt.Sprintf("%d", d.mirror.Status.VaultSource.LeaseDuration)
		}
	}

	if doCreate {
		if err := d.Create(ctx, destSecret); err != nil {
			logger.Error(err, "unable to create own secret for SecretMirror", "secret", destSecret)
			return err
		}
	} else {
		if err := d.Update(ctx, destSecret); err != nil {
			logger.Error(err, "unable to update dest secret for SecretMirror", "secret", destSecret)
			return err
		}
	}

	logger.Info(fmt.Sprintf("successfully mirrored secret %s/%s to %s/%s",
		secret.Namespace, secret.Name, destSecret.Namespace, destSecret.Name))

	return nil
}

func (d *NamespacesDest) getDestinationNamespaces() []string {
	return d.nsKeeper.FindMatchingNamespaces(types.NamespacedName{
		Namespace: d.mirror.Namespace,
		Name:      d.mirror.Name,
	})
}

func (d *NamespacesDest) validateAnnotations(ctx context.Context, secret *v1.Secret) bool {
	if secret == nil {
		return true
	}

	logger := log.FromContext(ctx)

	value := secret.Annotations[ownedByMirrorAnnotation]
	managedByMirrorValue := d.getManagedByMirrorValue()

	if value == managedByMirrorValue {
		return true
	}

	logger.Info(
		fmt.Sprintf("secret %s/%s found but is not managed by SecretMirror %s/%s",
			secret.Namespace,
			secret.Name,
			d.mirror.Namespace,
			d.mirror.Name,
		),
	)

	return false
}

func (d *NamespacesDest) getManagedByMirrorValue() string {
	return getManagedByMirrorValue(d.mirror.Namespace, d.mirror.Name)
}

func (d *NamespacesDest) Cleanup(ctx context.Context) error {
	namespaces := d.getDestinationNamespaces()

	d.nsKeeper.DeregisterNamespaceRegex(types.NamespacedName{
		Namespace: d.mirror.Namespace,
		Name:      d.mirror.Name,
	})

	for _, ns := range namespaces {
		if err := d.deleteOneSecret(ctx, types.NamespacedName{
			Namespace: ns,
			Name:      d.mirror.Spec.Source.Name,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (d *NamespacesDest) deleteOneSecret(ctx context.Context, name types.NamespacedName) error {
	logger := log.FromContext(ctx)

	var secret v1.Secret
	if err := d.Get(ctx, name, &secret); err != nil {
		return client.IgnoreNotFound(err)
	}

	if d.mirror.Spec.DeletePolicy == mirrorsv1alpha2.DeletePolicyDelete {
		correctValue := d.getManagedByMirrorValue()
		if val, ok := secret.Annotations[ownedByMirrorAnnotation]; !ok || val != correctValue {
			logger.Info(fmt.Sprintf("secret %s/%s is not managed by SecretMirror %s",
				secret.Namespace, secret.Name, correctValue))
			return nil
		}

		if err := d.Delete(ctx, &secret); err != nil {
			return client.IgnoreNotFound(err)
		}

		logger.Info(fmt.Sprintf("deleted secret %s/%s", secret.Namespace, secret.Name))
	} else {
		logger.Info(fmt.Sprintf("retaining secret %s/%s", secret.Namespace, secret.Name))
	}

	return nil
}
