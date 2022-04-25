package controllers

import (
	"context"
	"github.com/ktsstudio/mirrors/api/v1alpha2"
	"github.com/ktsstudio/mirrors/pkg/backend"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("SecretMirror", func() {

	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		SecretMirrorName      = "test-secret-mirror"
		SecretMirrorNamespace = "default"
		SourceSecretName      = "demo-secret"

		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	var (
		mirrorKey = types.NamespacedName{
			Name:      SecretMirrorName,
			Namespace: SecretMirrorNamespace,
		}
		secretData = map[string][]byte{
			"hello":   []byte("there"),
			"general": []byte("kenobi"),
		}
		makeTestMirror = func() *v1alpha2.SecretMirror {
			return &v1alpha2.SecretMirror{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha2.GroupVersion.String(),
					Kind:       "SecretMirror",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      SecretMirrorName,
					Namespace: SecretMirrorNamespace,
				},
				Spec: v1alpha2.SecretMirrorSpec{
					PollPeriodSeconds: 2,
					Source: v1alpha2.SecretMirrorSource{
						Name: SourceSecretName,
					},
					Destination: v1alpha2.SecretMirrorDestination{
						Namespaces: []string{
							`mirror-ns-\d+`,
						},
					},
				},
			}
		}
		makeTestMirrorRetain = func() *v1alpha2.SecretMirror {
			return &v1alpha2.SecretMirror{
				TypeMeta: metav1.TypeMeta{
					APIVersion: v1alpha2.GroupVersion.String(),
					Kind:       "SecretMirror",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      SecretMirrorName,
					Namespace: SecretMirrorNamespace,
				},
				Spec: v1alpha2.SecretMirrorSpec{
					PollPeriodSeconds: 2,
					DeletePolicy:      v1alpha2.DeletePolicyRetain,
					Source: v1alpha2.SecretMirrorSource{
						Name: SourceSecretName,
					},
					Destination: v1alpha2.SecretMirrorDestination{
						Namespaces: []string{
							`mirror-ns-\d+`,
						},
					},
				},
			}
		}

		createdResources []client.Object

		track = func(o client.Object) client.Object {
			createdResources = append(createdResources, o)
			return o
		}
	)

	BeforeEach(func() {
		logger.Info("resetting created resources list")
		createdResources = nil
	})

	AfterEach(func() {
		for i := len(createdResources) - 1; i >= 0; i-- {
			r := createdResources[i]
			key := client.ObjectKeyFromObject(r)

			_, isNamespace := r.(*v1.Namespace)
			if !isNamespace {
				logger.Info("deleting resource", "namespace", key.Namespace, "name", key.Name, "test", CurrentGinkgoTestDescription().FullTestText)
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, r))).To(Succeed())

				logger.Info("waiting for resource to disappear", "namespace", key.Namespace, "name", key.Name, "test", CurrentGinkgoTestDescription().FullTestText)
				Eventually(func() error {
					return k8sClient.Get(ctx, key, r)
				}, timeout, interval).Should(HaveOccurred())
				logger.Info("deleted resource", "namespace", key.Namespace, "name", key.Name, "test", CurrentGinkgoTestDescription().FullTestText)
			}
		}
	})

	Context("When creating with dest=namespaces & source does not exist", func() {
		It("Should become Active and copy secrets on the go", func() {
			By("Creating a mirror without a secret")

			Expect(k8sClient.Create(ctx, track(makeTestMirror()))).Should(Succeed())
			mirror := &v1alpha2.SecretMirror{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, mirrorKey, mirror)
				if err != nil {
					return false
				}
				return mirror.Status.MirrorStatus != ""
			}, timeout, interval).Should(BeTrue())

			Expect(mirror.Status.MirrorStatus).Should(Equal(v1alpha2.MirrorStatusPending))
			Expect(mirror.Spec.DeletePolicy).Should(Equal(v1alpha2.DeletePolicyDelete))
			Expect(mirror.Spec.Source.Type).Should(Equal(v1alpha2.SourceTypeSecret))
			Expect(mirror.Spec.Destination.Type).Should(Equal(v1alpha2.DestTypeNamespaces))
			Expect(mirror.Spec.Destination.Namespaces).Should(Equal([]string{
				`mirror-ns-\d+`,
			}))

			By("Creating a secret for the mirror")
			Expect(k8sClient.Create(ctx, track(&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      SourceSecretName,
					Namespace: SecretMirrorNamespace,
				},
				Data: secretData,
			}))).Should(Succeed())

			Eventually(func() string {
				f := &v1alpha2.SecretMirror{}
				_ = k8sClient.Get(context.Background(), mirrorKey, f)
				return string(f.Status.MirrorStatus)
			}, 40*time.Second, 10*time.Second).Should(Equal(string(v1alpha2.MirrorStatusActive)))
		})
	})

	Context("When creating with dest=namespaces & secret exists", func() {

		var mirror *v1alpha2.SecretMirror
		BeforeEach(func() {
			By("Creating a secret for mirror")
			Expect(k8sClient.Create(ctx, track(&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      SourceSecretName,
					Namespace: SecretMirrorNamespace,
				},
				Data: secretData,
			}))).Should(Succeed())

			By("Creating dest namespaces for mirror")
			_ = k8sClient.Create(ctx, track(&v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mirror-ns-1",
				},
			}))
			_ = k8sClient.Create(ctx, track(&v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mirror-ns-2",
				},
			}))
		})

		It("Should become Active when secret and dest ns exist", func() {
			By("Creating a mirror")
			Expect(k8sClient.Create(ctx, track(makeTestMirror()))).Should(Succeed())
			mirror = &v1alpha2.SecretMirror{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, mirrorKey, mirror)
				if err != nil {
					return false
				}
				return mirror.Status.MirrorStatus == v1alpha2.MirrorStatusActive
			}, timeout, interval).Should(BeTrue())

			Expect(string(mirror.Status.MirrorStatus)).Should(Equal(v1alpha2.MirrorStatusActive))

			By("Ensuring a secret has been copied successfully")
			secretCopy, err := backend.FetchSecret(ctx, k8sClient, types.NamespacedName{
				Name:      SourceSecretName,
				Namespace: "mirror-ns-1",
			})
			Expect(err).Should(Succeed())
			Expect(secretCopy.Data).Should(Equal(secretData))

			secretCopy2, err := backend.FetchSecret(ctx, k8sClient, types.NamespacedName{
				Name:      SourceSecretName,
				Namespace: "mirror-ns-2",
			})
			Expect(err).Should(Succeed())
			Expect(secretCopy2.Data).Should(Equal(secretData))
		})

		It("Should delete secrets when mirror is deleted", func() {
			By("Creating a mirror")
			Expect(k8sClient.Create(ctx, track(makeTestMirror()))).Should(Succeed())
			mirror = &v1alpha2.SecretMirror{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, mirrorKey, mirror)
				if err != nil {
					return false
				}
				return mirror.Status.MirrorStatus == v1alpha2.MirrorStatusActive
			}, timeout, interval).Should(BeTrue())

			By("Ensuring a secret has been copied successfully")
			secretCopy, err := backend.FetchSecret(ctx, k8sClient, types.NamespacedName{
				Name:      SourceSecretName,
				Namespace: "mirror-ns-1",
			})
			Expect(err).Should(Succeed())
			Expect(secretCopy.Data).Should(Equal(secretData))

			secretCopy2, err := backend.FetchSecret(ctx, k8sClient, types.NamespacedName{
				Name:      SourceSecretName,
				Namespace: "mirror-ns-2",
			})
			Expect(err).Should(Succeed())
			Expect(secretCopy2.Data).Should(Equal(secretData))

			By("deleting the mirror")
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, mirror))).To(Succeed())

			By("ensuring mirrored secrets do not exist")
			Eventually(func() bool {
				r := &v1.Secret{}
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name:      SourceSecretName,
					Namespace: "mirror-ns-1",
				}, r))
			}, timeout, interval).Should(BeTrue())

			Eventually(func() bool {
				r := &v1.Secret{}
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name:      SourceSecretName,
					Namespace: "mirror-ns-2",
				}, r))
			}, timeout, interval).Should(BeTrue())
		})

		It("Should not delete secrets when mirror is deleted and retain deletePolicy=Retain", func() {
			By("Creating a mirror")
			Expect(k8sClient.Create(ctx, track(makeTestMirrorRetain()))).Should(Succeed())
			mirror = &v1alpha2.SecretMirror{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, mirrorKey, mirror)
				if err != nil {
					return false
				}
				return mirror.Status.MirrorStatus == v1alpha2.MirrorStatusActive
			}, timeout, interval).Should(BeTrue())

			By("Ensuring a secret has been copied successfully")
			secretCopy, err := backend.FetchSecret(ctx, k8sClient, types.NamespacedName{
				Name:      SourceSecretName,
				Namespace: "mirror-ns-1",
			})
			Expect(err).Should(Succeed())
			Expect(secretCopy.Data).Should(Equal(secretData))

			secretCopy2, err := backend.FetchSecret(ctx, k8sClient, types.NamespacedName{
				Name:      SourceSecretName,
				Namespace: "mirror-ns-2",
			})
			Expect(err).Should(Succeed())
			Expect(secretCopy2.Data).Should(Equal(secretData))

			By("deleting the mirror")
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, mirror))).To(Succeed())

			By("ensuring mirrored secrets still exist")
			Eventually(func() error {
				r := &v1.Secret{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      SourceSecretName,
					Namespace: "mirror-ns-1",
				}, r)
			}, timeout, interval).Should(Succeed())

			Eventually(func() error {
				r := &v1.Secret{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      SourceSecretName,
					Namespace: "mirror-ns-2",
				}, r)
			}, timeout, interval).Should(Succeed())
		})
	})
})
