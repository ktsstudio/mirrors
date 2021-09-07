package backend

import (
	"context"
	"errors"
	"fmt"
	mirrorsv1alpha1 "github.com/ktsstudio/mirrors/api/v1alpha1"
	"github.com/ktsstudio/mirrors/pkg/nskeeper"
	"github.com/panjf2000/ants/v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"regexp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"time"
)

var (
	secretOwnerKey          = ".metadata.controller"
	apiGVStr                = mirrorsv1alpha1.GroupVersion.String()
	ErrSourceObjectNotFound = errors.New("source object not found")
)

type secretMirrorContext struct {
	backend *secretMirrorBackend

	secretMirror       *mirrorsv1alpha1.SecretMirror
	sourceSecret       *v1.Secret
	destNamespaceRegex *regexp.Regexp
}

func (c *secretMirrorContext) ObjectName() string {
	return c.secretMirror.Spec.Source.Name
}

func (c *secretMirrorContext) MirrorStatus() mirrorsv1alpha1.MirrorStatus {
	return c.secretMirror.Status.MirrorStatus
}

func (c *secretMirrorContext) PollPeriodDuration() time.Duration {
	return c.secretMirror.PollPeriodDuration()
}

func (c *secretMirrorContext) normalize() {
	if c.secretMirror.Spec.Destination.Namespace == "" && c.secretMirror.Spec.Destination.NamespaceRegex == "" {
		// trying to use pull mode
		c.secretMirror.Spec.Destination.Namespace = c.secretMirror.Namespace
	}

	if c.secretMirror.Spec.PollPeriodSeconds == 0 {
		c.secretMirror.Spec.PollPeriodSeconds = 3 * 60
	}
}

func (c *secretMirrorContext) Init(ctx context.Context, name types.NamespacedName) error {
	logger := log.FromContext(ctx)

	var secretMirror mirrorsv1alpha1.SecretMirror
	if err := c.backend.Client.Get(ctx, name, &secretMirror); err != nil {
		return client.IgnoreNotFound(err)
	}

	c.secretMirror = &secretMirror
	c.normalize()

	if c.secretMirror.Spec.Destination.Namespace == c.secretMirror.Namespace {
		// if SecretMirror deployed into the source
		return c.SetStatus(ctx, mirrorsv1alpha1.MirrorStatusActive)
	}

	sourceSecretName := types.NamespacedName{
		Namespace: c.secretMirror.Namespace,
		Name:      c.secretMirror.Spec.Source.Name,
	}

	var sourceSecret v1.Secret
	if err := c.backend.Get(ctx, sourceSecretName, &sourceSecret); err != nil {
		logger.Info(fmt.Sprintf("secret %s not found, waiting to appear", sourceSecretName))
		_ = c.SetStatusPending(ctx)
		return ErrSourceObjectNotFound
	}

	c.sourceSecret = &sourceSecret

	if c.secretMirror.Spec.Destination.NamespaceRegex != "" {
		regex, err := regexp.Compile(c.secretMirror.Spec.Destination.NamespaceRegex)
		if err != nil {
			return err
		}
		c.destNamespaceRegex = regex
	}

	return nil
}

// SetupOrRunFinalizer returns (stopReconciliation, error)
func (c *secretMirrorContext) SetupOrRunFinalizer(ctx context.Context) (bool, error) {
	logger := log.FromContext(ctx)

	// examine DeletionTimestamp to determine if object is under deletion
	if c.secretMirror.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !containsString(c.secretMirror.GetFinalizers(), mirrorsFinalizerName) {
			controllerutil.AddFinalizer(c.secretMirror, mirrorsFinalizerName)
			if err := c.backend.Update(ctx, c.secretMirror); err != nil {
				return false, err
			}
		}
	} else {
		// The object is being deleted
		if containsString(c.secretMirror.GetFinalizers(), mirrorsFinalizerName) {
			// our finalizer is present, so lets handle any external dependency
			if err := c.deleteExternalResources(ctx); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return false, err
			}
			logger.Info("deleted managed objects")

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(c.secretMirror, mirrorsFinalizerName)
			if err := c.backend.Update(ctx, c.secretMirror); err != nil {
				return false, err
			}
		}

		// Stop reconciliation as the item is being deleted
		return true, nil
	}

	return false, nil
}

func (c *secretMirrorContext) SetStatusPending(ctx context.Context) error {
	if c.secretMirror.Status.MirrorStatus == mirrorsv1alpha1.MirrorStatusPending {
		return nil
	}

	c.secretMirror.Status.MirrorStatus = mirrorsv1alpha1.MirrorStatusPending
	if c.secretMirror.Status.LastSyncTime.IsZero() {
		c.secretMirror.Status.LastSyncTime = metav1.Unix(0, 0)
	}
	if err := c.backend.Status().Update(ctx, c.secretMirror); err != nil {
		return err
	}
	return nil
}

func (c *secretMirrorContext) SetStatus(ctx context.Context, status mirrorsv1alpha1.MirrorStatus) error {
	if c.secretMirror.Status.MirrorStatus == status {
		return nil
	}

	c.secretMirror.Status.MirrorStatus = status
	c.secretMirror.Status.LastSyncTime = metav1.Now()
	return c.backend.Status().Update(ctx, c.secretMirror)
}

func (c *secretMirrorContext) GetDestinationNamespaces() ([]string, error) {
	if c.secretMirror.Spec.Destination.Namespace != "" {
		return []string{
			c.secretMirror.Spec.Destination.Namespace,
		}, nil
	}

	if c.destNamespaceRegex != nil {
		namespaces, err := getFilteredNamespaces(c.backend.nsKeeper, c.destNamespaceRegex)
		if err != nil {
			return nil, err
		}
		return namespaces, nil
	}

	return nil, nil
}

func (c *secretMirrorContext) deleteExternalResources(ctx context.Context) error {
	namespaces, err := c.GetDestinationNamespaces()
	if err != nil {
		return err
	}

	for _, ns := range namespaces {
		if err := c.deleteOne(ctx, types.NamespacedName{
			Namespace: ns,
			Name:      c.secretMirror.Spec.Source.Name,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *secretMirrorContext) deleteOne(ctx context.Context, name types.NamespacedName) error {
	logger := log.FromContext(ctx)

	var secret v1.Secret
	if err := c.backend.Get(ctx, name, &secret); err != nil {
		return client.IgnoreNotFound(err)
	}

	logger.Info(fmt.Sprintf("deleted secret %s/%s", secret.Namespace, secret.Name))

	if err := c.backend.Delete(ctx, &secret); err != nil {
		return client.IgnoreNotFound(err)
	}

	return nil
}

func (c *secretMirrorContext) SyncOne(ctx context.Context, dest types.NamespacedName) error {
	logger := log.FromContext(ctx)

	destSecret, err := c.fetchSecret(ctx, dest)
	if err != nil {
		return err
	}

	if !c.validateAnnotations(ctx, destSecret) {
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
			Type: c.sourceSecret.Type,
		}
		doCreate = true
	}

	if !secretDiffer(c.sourceSecret, destSecret) {
		logger.Info(fmt.Sprintf("secrets %s/%s and %s/%s are identical",
			c.sourceSecret.Namespace, c.sourceSecret.Name, destSecret.Namespace, destSecret.Name))
		return nil
	}

	copySecret(c.sourceSecret, destSecret)
	destSecret.Annotations[managedByMirrorAnnotation] = c.getManagedByMirrorValue()

	if doCreate {
		if err := c.backend.Create(ctx, destSecret); err != nil {
			logger.Error(err, "unable to create own secret for SecretMirror", "secret", destSecret)
			return err
		}
	} else {
		if err := c.backend.Update(ctx, destSecret); err != nil {
			logger.Error(err, "unable to update dest secret for SecretMirror", "secret", destSecret)
			return err
		}
	}

	logger.Info(fmt.Sprintf("successfully mirrored secret %s/%s to %s/%s",
		c.sourceSecret.Namespace, c.sourceSecret.Name, destSecret.Namespace, destSecret.Name))

	return nil
}

func (c *secretMirrorContext) fetchSecret(ctx context.Context, name types.NamespacedName) (*v1.Secret, error) {
	var secret v1.Secret
	if err := c.backend.Get(ctx, name, &secret); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	return &secret, nil
}

func (c *secretMirrorContext) validateAnnotations(ctx context.Context, secret *v1.Secret) bool {
	if secret == nil {
		return true
	}

	logger := log.FromContext(ctx)

	value := secret.Annotations[managedByMirrorAnnotation]
	managedByMirrorValue := c.getManagedByMirrorValue()

	if value == managedByMirrorValue {
		return true
	}

	logger.Info(
		fmt.Sprintf("secret %s/%s found but is not managed by SecretMirror %s/%s",
			secret.Namespace,
			secret.Name,
			c.secretMirror.Namespace,
			c.secretMirror.Name,
		),
	)

	return false
}

func (c *secretMirrorContext) getManagedByMirrorValue() string {
	return getManagedByMirrorValue(
		c.secretMirror.Namespace,
		c.secretMirror.Name,
	)
}

/// Backend

type secretMirrorBackend struct {
	client.Client
	nsKeeper *nskeeper.NSKeeper
	pool     *ants.Pool
}

func MakeSecretMirrorBackend(cli client.Client, nsKeeper *nskeeper.NSKeeper) (MirrorBackend, error) {
	pool, err := ants.NewPool(DefaultWorkerPoolSize)
	if err != nil {
		return nil, err
	}
	return &secretMirrorBackend{
		Client:   cli,
		nsKeeper: nsKeeper,
		pool:     pool,
	}, nil
}

func (b *secretMirrorBackend) Pool() *ants.Pool {
	return b.pool
}

func MustMakeSecretMirrorBackend(cli client.Client, nsKeeper *nskeeper.NSKeeper) MirrorBackend {
	backend, err := MakeSecretMirrorBackend(cli, nsKeeper)
	if err != nil {
		panic(err)
	}
	return backend
}

func (b *secretMirrorBackend) SetupWithManager(mgr ctrl.Manager) (*ctrl.Builder, error) {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1.Secret{}, secretOwnerKey, func(rawObj client.Object) []string {
		// grab the secret object, extract the owner...
		secret := rawObj.(*v1.Secret)
		owner := metav1.GetControllerOf(secret)
		if owner == nil {
			return nil
		}
		// ...make sure it's a SecretMirror...
		if owner.APIVersion != apiGVStr || owner.Kind != "SecretMirror" {
			return nil
		}

		// ...and if so, return it
		return []string{owner.Name}
	}); err != nil {
		return nil, err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&mirrorsv1alpha1.SecretMirror{}).
		Owns(&v1.Secret{}), nil
}

func (b *secretMirrorBackend) Init(ctx context.Context, name types.NamespacedName) (MirrorContext, error) {
	mirrorContext := &secretMirrorContext{
		backend: b,
	}
	if err := mirrorContext.Init(ctx, name); err != nil {
		return nil, err
	}

	if mirrorContext.secretMirror == nil {
		return nil, nil
	}
	return mirrorContext, nil
}

func (b *secretMirrorBackend) Cleanup() {
	b.pool.Release()
}
