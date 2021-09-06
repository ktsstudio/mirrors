package backend

import (
	"context"
	"fmt"
	mirrorsv1alpha1 "github.com/ktsstudio/mirrors/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"regexp"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"time"
)

type secretMirrorBackend struct {
	client.Client
	secretMirror       *mirrorsv1alpha1.SecretMirror
	sourceSecret       *v1.Secret
	destNamespaceRegex *regexp.Regexp
}

func MakeSecretMirrorBackend(ctx context.Context, cli client.Client, name types.NamespacedName) (MirrorBackend, error) {
	var secretMirror mirrorsv1alpha1.SecretMirror
	if err := cli.Get(ctx, name, &secretMirror); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	b := &secretMirrorBackend{
		Client:       cli,
		secretMirror: &secretMirror,
	}
	b.normalize()
	return b, nil
}

func (b *secretMirrorBackend) MirrorStatus() mirrorsv1alpha1.MirrorStatus {
	return b.secretMirror.Status.MirrorStatus
}

func (b *secretMirrorBackend) PollPeriodDuration() time.Duration {
	return b.secretMirror.PollPeriodDuration()
}

func (b *secretMirrorBackend) SetStatusPending(ctx context.Context) error {
	if b.secretMirror.Status.MirrorStatus == mirrorsv1alpha1.MirrorStatusPending {
		return nil
	}

	b.secretMirror.Status.MirrorStatus = mirrorsv1alpha1.MirrorStatusPending
	if b.secretMirror.Status.LastSyncTime.IsZero() {
		b.secretMirror.Status.LastSyncTime = metav1.Unix(0, 0)
	}
	if err := b.Status().Update(ctx, b.secretMirror); err != nil {
		return err
	}
	return nil
}

func (b *secretMirrorBackend) SetStatus(ctx context.Context, status mirrorsv1alpha1.MirrorStatus) error {
	if b.secretMirror.Status.MirrorStatus == status {
		return nil
	}

	b.secretMirror.Status.MirrorStatus = status
	b.secretMirror.Status.LastSyncTime = metav1.Now()
	return b.Status().Update(ctx, b.secretMirror)
}

func (b *secretMirrorBackend) Init(ctx context.Context) error {
	dest := b.secretMirror.Spec.Destination
	src := b.secretMirror.Spec.Source

	if dest.Namespace == src.Namespace {
		// if SecretMirror deployed into the source
		return b.SetStatus(ctx, mirrorsv1alpha1.MirrorStatusActive)
	}

	sourceSecretName := types.NamespacedName{
		Namespace: src.Namespace,
		Name:      src.Name,
	}

	var sourceSecret v1.Secret
	if err := b.Get(ctx, sourceSecretName, &sourceSecret); err != nil {
		_ = b.SetStatusPending(ctx)
		return err
	}

	b.sourceSecret = &sourceSecret

	if b.secretMirror.Spec.Destination.NamespaceRegex != "" {
		regex, err := regexp.Compile(b.secretMirror.Spec.Destination.NamespaceRegex)
		if err != nil {
			return err
		}
		b.destNamespaceRegex = regex
	}

	return nil
}

// SetupOrRunFinalizer returns (stopReconciliation, error)
func (b *secretMirrorBackend) SetupOrRunFinalizer(ctx context.Context) (bool, error) {
	logger := log.FromContext(ctx)

	// examine DeletionTimestamp to determine if object is under deletion
	if b.secretMirror.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !containsString(b.secretMirror.GetFinalizers(), mirrorsFinalizerName) {
			controllerutil.AddFinalizer(b.secretMirror, mirrorsFinalizerName)
			if err := b.Update(ctx, b.secretMirror); err != nil {
				return false, err
			}
		}
	} else {
		// The object is being deleted
		if containsString(b.secretMirror.GetFinalizers(), mirrorsFinalizerName) {
			// our finalizer is present, so lets handle any external dependency
			if err := b.deleteExternalResources(ctx); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return false, err
			}
			logger.Info("deleted managed objects")

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(b.secretMirror, mirrorsFinalizerName)
			if err := b.Update(ctx, b.secretMirror); err != nil {
				return false, err
			}
		}

		// Stop reconciliation as the item is being deleted
		return true, nil
	}

	return false, nil
}

func (b *secretMirrorBackend) normalize() {
	if b.secretMirror.Spec.Source.Namespace == "" {
		b.secretMirror.Spec.Source.Namespace = b.secretMirror.Namespace
	}

	if b.secretMirror.Spec.Destination.Namespace == "" && b.secretMirror.Spec.Destination.NamespaceRegex == "" {
		// trying to use pull mode
		b.secretMirror.Spec.Destination.Namespace = b.secretMirror.Namespace
	}
}

func (b *secretMirrorBackend) deleteExternalResources(ctx context.Context) error {
	dest := b.secretMirror.Spec.Destination
	if dest.Namespace != "" {
		return b.deleteOne(ctx, types.NamespacedName{
			Namespace: dest.Namespace,
			Name:      b.secretMirror.Spec.Source.Name,
		})
	} else if b.destNamespaceRegex != nil {
		namespaces, err := getNamespaces(ctx, b.Client)
		if err != nil {
			return err
		}
		namespaces = filterNamespacesByRegex(namespaces, b.destNamespaceRegex)

		for _, ns := range namespaces {
			if err := b.deleteOne(ctx, types.NamespacedName{
				Namespace: ns.Name,
				Name:      b.secretMirror.Spec.Source.Name,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *secretMirrorBackend) deleteOne(ctx context.Context, name types.NamespacedName) error {
	logger := log.FromContext(ctx)

	var secret v1.Secret
	if err := b.Get(ctx, name, &secret); err != nil {
		return client.IgnoreNotFound(err)
	}

	logger.Info(fmt.Sprintf("deleted secret %s/%s", secret.Namespace, secret.Name))

	if err := b.Delete(ctx, &secret); err != nil {
		return client.IgnoreNotFound(err)
	}

	return nil
}

func (b *secretMirrorBackend) Sync(ctx context.Context) error {
	logger := log.FromContext(ctx)

	dest := b.secretMirror.Spec.Destination
	if dest.Namespace != "" {
		if err := b.syncOne(ctx, types.NamespacedName{
			Namespace: dest.Namespace,
			Name:      b.sourceSecret.Name,
		}); err != nil {
			return err
		}
	} else if b.destNamespaceRegex != nil {
		namespaces, err := getNamespaces(ctx, b.Client)
		if err != nil {
			return err
		}
		namespaces = filterNamespacesByRegex(namespaces, b.destNamespaceRegex)

		for _, ns := range namespaces {
			if err := b.syncOne(ctx, types.NamespacedName{
				Namespace: ns.Name,
				Name:      b.sourceSecret.Name,
			}); err != nil {
				return err
			}
		}
	}

	if err := b.SetStatus(ctx, mirrorsv1alpha1.MirrorStatusActive); err != nil {
		logger.Error(err, "unable to update SecretMirror status")
		return err
	}
	return nil
}

func (b *secretMirrorBackend) syncOne(ctx context.Context, dest types.NamespacedName) error {
	logger := log.FromContext(ctx)

	destSecret, err := b.fetchSecret(ctx, dest)
	if err != nil {
		return err
	}

	if err := b.validateAnnotations(ctx, destSecret); err != nil {
		return err
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
			Type: b.sourceSecret.Type,
		}
		doCreate = true
	}

	if !secretDiffer(b.sourceSecret, destSecret) {
		logger.Info(fmt.Sprintf("secrets %s/%s and %s/%s are identical",
			b.sourceSecret.Namespace, b.sourceSecret.Name, destSecret.Namespace, destSecret.Name))
		return b.SetStatus(ctx, mirrorsv1alpha1.MirrorStatusActive)
	}

	copySecret(b.sourceSecret, destSecret)
	destSecret.Annotations[managedByMirrorAnnotation] = b.getManagedByMirrorValue()

	if doCreate {
		if err := b.Create(ctx, destSecret); err != nil {
			logger.Error(err, "unable to create own secret for SecretMirror", "secret", destSecret)
			_ = b.SetStatus(ctx, mirrorsv1alpha1.MirrorStatusError)
			return err
		}
	} else {
		if err := b.Update(ctx, destSecret); err != nil {
			logger.Error(err, "unable to update dest secret for SecretMirror", "secret", destSecret)
			_ = b.SetStatus(ctx, mirrorsv1alpha1.MirrorStatusError)
			return err
		}
	}

	logger.Info(fmt.Sprintf("successfully mirrored secret %s/%s to %s/%s",
		b.sourceSecret.Namespace, b.sourceSecret.Name, destSecret.Namespace, destSecret.Name))

	return nil
}

func (b *secretMirrorBackend) fetchSecret(ctx context.Context, name types.NamespacedName) (*v1.Secret, error) {
	var secret v1.Secret
	if err := b.Get(ctx, name, &secret); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	return &secret, nil
}

func (b *secretMirrorBackend) validateAnnotations(ctx context.Context, secret *v1.Secret) error {
	if secret == nil {
		return nil
	}

	logger := log.FromContext(ctx)

	value := secret.Annotations[managedByMirrorAnnotation]
	managedByMirrorValue := b.getManagedByMirrorValue()

	if value == managedByMirrorValue {
		return nil
	}

	logger.Error(
		notManagedByMirror,
		fmt.Sprintf("secret %s/%s found but is not managed by SecretMirror %s/%s",
			b.secretMirror.Spec.Source.Namespace,
			b.secretMirror.Spec.Source.Name,
			b.secretMirror.Namespace,
			b.secretMirror.Name,
		),
	)

	_ = b.SetStatus(ctx, mirrorsv1alpha1.MirrorStatusError)
	return notManagedByMirror
}

func (b *secretMirrorBackend) getManagedByMirrorValue() string {
	return getManagedByMirrorValue(
		b.secretMirror.Namespace,
		b.secretMirror.Name,
	)
}
