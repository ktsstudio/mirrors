package backend

import (
	"context"
	"encoding/base64"
	"fmt"
	mirrorsv1alpha2 "github.com/ktsstudio/mirrors/api/v1alpha2"
	"github.com/ktsstudio/mirrors/pkg/nskeeper"
	"github.com/ktsstudio/mirrors/pkg/silenterror"
	"github.com/panjf2000/ants/v2"
	"golang.org/x/sync/errgroup"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"regexp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sync"
	"time"
)

var (
	secretOwnerKey = ".metadata.controller"
	apiGVStr       = mirrorsv1alpha2.GroupVersion.String()
)

type SecretMirrorContext struct {
	backend *SecretMirrorBackend

	secretMirror *mirrorsv1alpha2.SecretMirror
	sourceSecret *v1.Secret
}

func (c *SecretMirrorContext) MirrorStatus() mirrorsv1alpha2.MirrorStatus {
	return c.secretMirror.Status.MirrorStatus
}

func (c *SecretMirrorContext) PollPeriodDuration() time.Duration {
	return c.secretMirror.PollPeriodDuration()
}

func (c *SecretMirrorContext) Init(ctx context.Context, name types.NamespacedName) error {
	logger := log.FromContext(ctx)
	logger.Info(fmt.Sprintf("reconciling secret mirror %s", name))

	var secretMirror mirrorsv1alpha2.SecretMirror
	if err := c.backend.Client.Get(ctx, name, &secretMirror); err != nil {
		return client.IgnoreNotFound(err)
	}

	c.secretMirror = &secretMirror
	c.secretMirror.Default()

	if c.secretMirror.Spec.Source.Type == mirrorsv1alpha2.SourceTypeSecret {
		sourceSecretName := types.NamespacedName{
			Namespace: c.secretMirror.Namespace,
			Name:      c.secretMirror.Spec.Source.Name,
		}

		var sourceSecret v1.Secret
		if err := c.backend.Get(ctx, sourceSecretName, &sourceSecret); err != nil {
			_ = c.SetStatus(ctx, mirrorsv1alpha2.MirrorStatusPending)
			c.backend.Recorder.Eventf(c.secretMirror, "Warning", "NoSecret",
				"Secret %s not found, waiting to appear", sourceSecretName,
			)
			return silenterror.FmtWithRequeue(30*time.Second, "secret %s not found, waiting to appear", sourceSecretName)
		}

		c.sourceSecret = &sourceSecret

	} else if c.secretMirror.Spec.Source.Type == mirrorsv1alpha2.SourceTypeVault {
		var sourceSecret v1.Secret

		vault, err := c.makeVaultBackend(ctx, c.secretMirror.Spec.Source.Vault)
		if err != nil {
			return err
		}

		data, err := c.vaultRetrieveSecretData(vault, c.secretMirror.Spec.Source.Vault.Path)
		if err != nil {
			return err
		}

		sourceSecret.Data = data
		sourceSecret.Namespace = "<vault>"
		sourceSecret.Name = c.secretMirror.Spec.Source.Vault.Path
		c.sourceSecret = &sourceSecret

	} else {
		return fmt.Errorf("source.type %s is unsupported", c.secretMirror.Spec.Source.Type)
	}

	if len(c.secretMirror.Spec.Destination.Namespaces) > 0 {
		regexps := make([]*regexp.Regexp, 0, len(c.secretMirror.Spec.Destination.Namespaces))
		for _, regexRaw := range c.secretMirror.Spec.Destination.Namespaces {
			regex, err := regexp.Compile(regexRaw)
			if err != nil {
				return err
			}
			regexps = append(regexps, regex)
		}

		c.backend.nsKeeper.RegisterNamespaceRegex(name, regexps)
	}

	return nil
}

func (c *SecretMirrorContext) vaultRetrieveSecretData(vault VaultBackend, path string) (map[string][]byte, error) {
	vaultData, err := vault.RetrieveData(path)
	if err != nil {
		return nil, err
	}

	data := make(map[string][]byte, len(vaultData))
	for k, v := range vaultData {
		stringValue, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("vault key %s contains non-string value", k)
		}
		var value []byte
		// try to decode base64 strings
		decodedValue, err := base64.StdEncoding.DecodeString(stringValue)
		if err == nil {
			// indeed a base64 string
			value = decodedValue
		} else {
			value = []byte(stringValue)
		}
		data[k] = value
	}
	return data, nil
}

// SetupOrRunFinalizer returns (stopReconciliation, error)
func (c *SecretMirrorContext) SetupOrRunFinalizer(ctx context.Context) (bool, error) {
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
			if c.secretMirror.Spec.Destination.Type == mirrorsv1alpha2.DestTypeNamespaces {
				if err := c.deleteExternalResources(ctx); err != nil {
					// if fail to delete the external dependency here, return with error
					// so that it can be retried
					return false, err
				}
				if c.secretMirror.Spec.DeletePolicy == mirrorsv1alpha2.DeletePolicyDelete {
					logger.Info("deleted managed objects")
				} else {
					logger.Info("retaining all managed secrets")
				}
			}

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

func (c *SecretMirrorContext) SetStatus(ctx context.Context, status mirrorsv1alpha2.MirrorStatus) error {
	if c.secretMirror.Status.MirrorStatus == status {
		return nil
	}

	c.secretMirror.Status.MirrorStatus = status
	if status == mirrorsv1alpha2.MirrorStatusPending {
		if c.secretMirror.Status.LastSyncTime.IsZero() {
			c.secretMirror.Status.LastSyncTime = metav1.Unix(0, 0)
		}
	} else {
		c.secretMirror.Status.LastSyncTime = metav1.Now()
	}
	return c.backend.Status().Update(ctx, c.secretMirror)
}

func (c *SecretMirrorContext) getDestinationNamespaces() []string {
	return c.backend.nsKeeper.FindMatchingNamespaces(types.NamespacedName{
		Namespace: c.secretMirror.Namespace,
		Name:      c.secretMirror.Name,
	})
}

func (c *SecretMirrorContext) deleteExternalResources(ctx context.Context) error {
	if c.secretMirror.Spec.Destination.Type == mirrorsv1alpha2.DestTypeNamespaces {
		namespaces := c.getDestinationNamespaces()

		c.backend.nsKeeper.DeregisterNamespaceRegex(types.NamespacedName{
			Namespace: c.secretMirror.Namespace,
			Name:      c.secretMirror.Name,
		})

		for _, ns := range namespaces {
			if err := c.deleteOneSecret(ctx, types.NamespacedName{
				Namespace: ns,
				Name:      c.secretMirror.Spec.Source.Name,
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *SecretMirrorContext) deleteOneSecret(ctx context.Context, name types.NamespacedName) error {
	logger := log.FromContext(ctx)

	var secret v1.Secret
	if err := c.backend.Get(ctx, name, &secret); err != nil {
		return client.IgnoreNotFound(err)
	}

	if c.secretMirror.Spec.DeletePolicy == mirrorsv1alpha2.DeletePolicyDelete {
		correctValue := c.getManagedByMirrorValue()
		if val, ok := secret.Annotations[ownedByMirrorAnnotation]; !ok || val != correctValue {
			logger.Info(fmt.Sprintf("secret %s/%s is not managed by SecretMirror %s",
				secret.Namespace, secret.Name, correctValue))
			return nil
		}

		logger.Info(fmt.Sprintf("deleted secret %s/%s", secret.Namespace, secret.Name))
	} else {
		logger.Info(fmt.Sprintf("retaining secret %s/%s", secret.Namespace, secret.Name))
	}

	if err := c.backend.Delete(ctx, &secret); err != nil {
		return client.IgnoreNotFound(err)
	}

	return nil
}

func (c *SecretMirrorContext) syncOneToNamespace(ctx context.Context, dest types.NamespacedName) error {
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

	if c.secretMirror.Spec.Source.Type == mirrorsv1alpha2.SourceTypeSecret {
		if c.sourceSecret.GetResourceVersion() == destSecret.Annotations[parentVersionAnnotation] {
			logger.Info(fmt.Sprintf("secrets %s/%s and %s/%s have same resource version",
				c.sourceSecret.Namespace, c.sourceSecret.Name, destSecret.Namespace, destSecret.Name))
			return nil
		}
	}

	if !secretDiffer(c.sourceSecret, destSecret) {
		logger.Info(fmt.Sprintf("secrets %s/%s and %s/%s are identical",
			c.sourceSecret.Namespace, c.sourceSecret.Name, destSecret.Namespace, destSecret.Name))
		return nil
	}

	copySecret(c.sourceSecret, destSecret)
	destSecret.Annotations[ownedByMirrorAnnotation] = c.getManagedByMirrorValue()
	destSecret.Annotations[lastSyncAnnotation] = metav1.Now().String()
	destSecret.Annotations[parentVersionAnnotation] = c.sourceSecret.ResourceVersion
	destSecret.Annotations[sourceTypeAnnotation] = string(c.secretMirror.Spec.Source.Type)
	if c.secretMirror.Spec.Source.Type == mirrorsv1alpha2.SourceTypeVault {
		destSecret.Annotations[vaultPathAnnotation] = c.secretMirror.Spec.Source.Vault.Path
	}

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

func (c *SecretMirrorContext) syncToNamespaces(ctx context.Context) error {
	logger := log.FromContext(ctx)

	wg := &sync.WaitGroup{}
	g := &errgroup.Group{}
	for _, ns := range c.getDestinationNamespaces() {
		ns := ns
		wg.Add(1)

		if err := c.backend.pool.Submit(func() {
			g.Go(func() error {
				defer wg.Done()
				return c.syncOneToNamespace(ctx, types.NamespacedName{
					Namespace: ns,
					Name:      c.secretMirror.Spec.Source.Name,
				})
			})
		}); err != nil {
			return err
		}
	}
	wg.Wait()

	if err := g.Wait(); err != nil {
		logger.Error(err, "unable to sync some objects")
		c.backend.Recorder.Eventf(c.secretMirror, "Warning", "Error",
			"Unable to sync some objects: %s", err,
		)
		_ = c.SetStatus(ctx, mirrorsv1alpha2.MirrorStatusError)
		return err
	}

	c.backend.Recorder.Eventf(c.secretMirror, "Normal", "Synced",
		"Synced secret %s/%s to namespaces", c.sourceSecret.Namespace, c.sourceSecret.Name,
	)

	if err := c.SetStatus(ctx, mirrorsv1alpha2.MirrorStatusActive); err != nil {
		logger.Error(err, "unable to update status")
		return err
	}
	return nil
}

func (c *SecretMirrorContext) syncToVault(ctx context.Context) error {
	logger := log.FromContext(ctx)

	if len(c.sourceSecret.Data) == 0 {
		return silenterror.Fmt("no data in source secret")
	}

	vault, err := c.makeVaultBackend(ctx, c.secretMirror.Spec.Destination.Vault)
	if err != nil {
		c.backend.Recorder.Eventf(c.secretMirror, "Warning", "VaultError",
			"Error setting up vault: %s", err,
		)
		return err
	}

	vaultData, err := c.vaultRetrieveSecretData(vault, c.secretMirror.Spec.Destination.Vault.Path)
	if err != nil {
		c.backend.Recorder.Eventf(c.secretMirror, "Warning", "VaultError",
			"Error retrieving secret from vault: %s", err,
		)
		return err
	}

	if !dataDiffer(c.sourceSecret.Data, vaultData) {
		logger.Info(fmt.Sprintf("secrets %s/%s and <vault>/%s are identical",
			c.sourceSecret.Namespace, c.sourceSecret.Name, c.secretMirror.Spec.Destination.Vault.Path))
		return nil
	}

	if err := vault.WriteData(c.secretMirror.Spec.Destination.Vault.Path, map[string]interface{}{
		"data": c.sourceSecret.Data,
	}); err != nil {
		c.backend.Recorder.Eventf(c.secretMirror, "Warning", "VaultError",
			"Error syncing to vault: %s", err,
		)
		return err
	}

	logger.Info("successfully synced secret to vault", "secretmirror", types.NamespacedName{
		Namespace: c.secretMirror.Namespace,
		Name:      c.secretMirror.Name,
	})

	return nil
}

func (c *SecretMirrorContext) makeVaultBackend(ctx context.Context, v *mirrorsv1alpha2.VaultSpec) (VaultBackend, error) {
	logger := log.FromContext(ctx)

	vault, err := c.backend.vaultBackendMaker(v.Addr)
	if err != nil {
		return nil, err
	}

	if v.AuthType() == mirrorsv1alpha2.VaultAuthTypeToken {
		ns := v.Auth.Token.SecretRef.Namespace
		if ns == "" {
			ns = c.secretMirror.Namespace
		}
		tokenSecretName := types.NamespacedName{
			Name:      v.Auth.Token.SecretRef.Name,
			Namespace: ns,
		}
		tokenSecret, err := c.fetchSecret(ctx, tokenSecretName)
		if err != nil {
			return nil, err
		}

		if tokenSecret == nil {
			return nil, silenterror.Fmt("secret %s for vault token not found", tokenSecretName)
		}

		token, exists := tokenSecret.Data[v.Auth.Token.TokenKey]
		if !exists {
			return nil, silenterror.Fmt("cannot find token under secret %s and key %s", tokenSecretName, v.Auth.Token.TokenKey)
		}
		vault.SetToken(string(token))

	} else if v.AuthType() == mirrorsv1alpha2.VaultAuthTypeAppRole {
		ns := v.Auth.AppRole.SecretRef.Namespace
		if ns == "" {
			ns = c.secretMirror.Namespace
		}
		appRoleSecretName := types.NamespacedName{
			Name:      v.Auth.AppRole.SecretRef.Name,
			Namespace: ns,
		}
		appRoleSecret, err := c.fetchSecret(ctx, appRoleSecretName)
		if err != nil {
			return nil, err
		}

		if appRoleSecret == nil {
			return nil, silenterror.Fmt("secret %s for vault approle login not found", appRoleSecretName)
		}

		roleID, exists := appRoleSecret.Data[v.Auth.AppRole.RoleIDKey]
		if !exists {
			return nil, silenterror.Fmt("cannot find roleID under secret %s and key %s", appRoleSecretName, v.Auth.AppRole.RoleIDKey)
		}
		secretID, exists := appRoleSecret.Data[v.Auth.AppRole.SecretIDKey]
		if !exists {
			return nil, silenterror.Fmt("cannot find secretID under secret %s and key %s", appRoleSecretName, v.Auth.AppRole.SecretIDKey)
		}
		if err := vault.LoginAppRole(v.Auth.AppRole.AppRolePath, string(roleID), string(secretID)); err != nil {
			return nil, silenterror.Fmt("error logging in to vault via approle: %s", err)
		}
	}

	logger.Info("successfully logged in to vault", "addr", v.Addr, "authType", v.AuthType())

	return vault, nil
}

func (c *SecretMirrorContext) Sync(ctx context.Context) error {
	logger := log.FromContext(ctx)

	if c.secretMirror.Spec.Destination.Type == mirrorsv1alpha2.DestTypeNamespaces {
		return c.syncToNamespaces(ctx)
	}

	if c.secretMirror.Spec.Destination.Type == mirrorsv1alpha2.DestTypeVault {
		if err := c.syncToVault(ctx); err != nil {
			_ = c.SetStatus(ctx, mirrorsv1alpha2.MirrorStatusError)
			return err
		}
		c.backend.Recorder.Eventf(c.secretMirror, "Normal", "Synced",
			"Synced secret %s/%s to vault", c.sourceSecret.Namespace, c.sourceSecret.Name,
		)
		if err := c.SetStatus(ctx, mirrorsv1alpha2.MirrorStatusActive); err != nil {
			logger.Error(err, "unable to update status")
			return err
		}
		return nil
	}

	return fmt.Errorf("unknown destination type: %s", c.secretMirror.Spec.Destination.Type)
}

func (c *SecretMirrorContext) fetchSecret(ctx context.Context, name types.NamespacedName) (*v1.Secret, error) {
	var secret v1.Secret
	if err := c.backend.Get(ctx, name, &secret); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	return &secret, nil
}

func (c *SecretMirrorContext) validateAnnotations(ctx context.Context, secret *v1.Secret) bool {
	if secret == nil {
		return true
	}

	logger := log.FromContext(ctx)

	value := secret.Annotations[ownedByMirrorAnnotation]
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

func (c *SecretMirrorContext) getManagedByMirrorValue() string {
	return getManagedByMirrorValue(
		c.secretMirror.Namespace,
		c.secretMirror.Name,
	)
}

/// Backend

type VaultBackendMakerFunc func(addr string) (VaultBackend, error)
type SecretMirrorBackend struct {
	client.Client
	nsKeeper          *nskeeper.NSKeeper
	pool              *ants.Pool
	vaultBackendMaker VaultBackendMakerFunc
	Recorder          record.EventRecorder
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

	if mirrorContext.secretMirror == nil {
		return nil, nil
	}
	return mirrorContext, nil
}

func (b *SecretMirrorBackend) Cleanup() {
	b.pool.Release()
}
