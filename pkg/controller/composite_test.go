package controller

import (
	"context"
	"encoding/json"
	"metacontroller/pkg/apis/metacontroller/v1alpha1"
	v1 "metacontroller/pkg/controller/composite/api/v1"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

var _ = Describe("CompositeController controller", func() {
	ctx := context.Background()

	// Define utility constants
	const (
		timeout = time.Second * 10
		// duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	// Context("SyncWebhook", func() {
	// 	It("Should be trigger the webhook and passes the request/resonse", func() {
	// 		By("By creating a Namespace")
	// 		ns := "test-sync-webhook"
	// 		lookupKey := types.NamespacedName{Name: "test-sync-webhook", Namespace: ns}
	// 		labels := map[string]string{
	// 			"test": "test-sync-webhook",
	// 		}
	// 		Expect(k8sClient.Create(ctx, ComposeNamespace(ns))).Should(Succeed())

	// 		By("By creating a Parent CRD")
	// 		parentCRD := ComposeCRD("TestSyncWebhookParent", apiextensions.NamespaceScoped)
	// 		Expect(k8sClient.Create(ctx, parentCRD)).Should(Succeed())

	// 		By("By creating a Child CRD")
	// 		childCRD := ComposeCRD("TestSyncWebhookChild", apiextensions.NamespaceScoped)
	// 		Expect(k8sClient.Create(ctx, childCRD)).Should(Succeed())

	// 		By("By serving a Webhook Endpoint")
	// 		hook := ServeWebhook(func(body []byte) ([]byte, error) {
	// 			req := v1.CompositeHookRequest{}
	// 			if err := json.Unmarshal(body, &req); err != nil {
	// 				return nil, err
	// 			}
	// 			// As a simple test of request/response content,
	// 			// just create a child with the same name as the parent.
	// 			child := UnstructuredCRD(childCRD, req.Parent.GetName())
	// 			child.SetLabels(labels)
	// 			resp := v1.CompositeHookResponse{
	// 				Children: []*unstructured.Unstructured{child},
	// 			}

	// 			return json.Marshal(resp)
	// 		})
	// 		hook.Close()

	// 		By("By creating a Composite Controller")
	// 		cc := createCompositeController("test-sync-webhook", hook.URL, "", CRDResourceRule(parentCRD), CRDResourceRule(childCRD), nil)
	// 		Expect(k8sClient.Create(ctx, cc)).Should(Succeed())

	// 		By("By creating a Custom Resource")
	// 		parent := UnstructuredCRD(parentCRD, lookupKey.Name)
	// 		unstructured.SetNestedStringMap(parent.Object, labels, "spec", "selector", "matchLabels")
	// 		parent.SetNamespace(lookupKey.Namespace)
	// 		Expect(k8sClient.Create(ctx, parent)).Should(Succeed())

	// 		By("By looking for the created Child Resource")
	// 		createdChild := UnstructuredCRD(childCRD, lookupKey.Name)
	// 		Eventually(func() bool {
	// 			err := k8sClient.Get(ctx, lookupKey, createdChild)
	// 			if err != nil {
	// 				return false
	// 			}
	// 			return true
	// 		}, timeout, interval).Should(BeTrue())
	// 	})
	// })

	Context("CascadingDelete", func() {
		It("Should be trigger the webhook and passes the request/resonse", func() {
			By("By creating a Namespace")
			ns := "test-cascading-delete"
			lookupKey := types.NamespacedName{Name: "test-cascading-delete", Namespace: ns}
			labels := map[string]string{
				"test": "test-cascading-delete",
			}
			Expect(k8sClient.Create(ctx, ComposeNamespace(ns))).Should(Succeed())

			By("By creating a Parent CRD")
			parentCRD := ComposeCRD("TestCascadingDeleteParent", apiextensions.NamespaceScoped)
			Expect(k8sClient.Create(ctx, parentCRD)).Should(Succeed())

			hook := ServeWebhook(func(body []byte) ([]byte, error) {
				req := v1.CompositeHookRequest{}
				if err := json.Unmarshal(body, &req); err != nil {
					return nil, err
				}
				resp := v1.CompositeHookRequest{}
				if replicas, _, _ := unstructured.NestedInt64(req.Parent.Object, "spec", "replicas"); replicas > 0 {
					// Create a child batch/v1 Job if requested.
					// For backward compatibility, the server-side default on that API is
					// non-cascading deletion (don't delete Pods).
					// So we can use this as a test case for whether we are correctly requesting
					// cascading deletion.
					child := UnstructuredJSON("batch/v1", "Job", "test-cascading-delete", `{
						"spec": {
							"template": {
								"spec": {
									"restartPolicy": "Never",
									"containers": [
										{
											"name": "pi",
											"image": "perl"
										}
									]
								}
							}
						}
					}`)
					child.SetLabels(labels)
					resp.Children.Insert(resp.Parent, child)
				}

				return json.Marshal(resp)
			})
			defer hook.Close()

			By("By creating a Composite Controller")
			cc := createCompositeController("test-cascading-delete", hook.URL, "", CRDResourceRule(parentCRD), &v1alpha1.ResourceRule{APIVersion: "batch/v1", Resource: "jobs"}, nil)
			Expect(k8sClient.Create(ctx, cc)).Should(Succeed())

			By("By creating a Custom Resource")
			parent := UnstructuredCRD(parentCRD, lookupKey.Name)
			unstructured.SetNestedStringMap(parent.Object, labels, "spec", "selector", "matchLabels")
			unstructured.SetNestedField(parent.Object, int64(1), "spec", "replicas")
			parent.SetNamespace(lookupKey.Namespace)
			Expect(k8sClient.Create(ctx, parent)).Should(Succeed())

			By("By waiting for the created Child Resource")
			createdChild := &batchv1.Job{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookupKey, createdChild)
				if err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())

			By("By updating the parent to set replicas=0")
			// Now that child exists, tell parent to delete it.
			unstructured.SetNestedField(parent.Object, int64(0), "spec", "replicas")
			Expect(k8sClient.Update(ctx, parent)).Should(Succeed())

			// Make sure the child gets actually deleted, which means no GC finalizers got
			// added to it. Note that we don't actually run the GC in this integration
			// test env, so we don't need to worry about the GC racing us to process the
			// finalizers.
			existingChild := &batchv1.Job{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookupKey, existingChild)
				if err != nil {
					return apierrors.IsNotFound(err)
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})
	})
})

func createCompositeController(name, syncHookURL string, customizeHookUrl string, parentRule, childRule *v1alpha1.ResourceRule, labels *map[string]string) *v1alpha1.CompositeController {
	childResources := []v1alpha1.CompositeControllerChildResourceRule{}
	if childRule != nil {
		childResources = append(childResources, v1alpha1.CompositeControllerChildResourceRule{ResourceRule: *childRule})
	}

	var customizeHook *v1alpha1.Hook
	if len(customizeHookUrl) != 0 {
		customizeHook = &v1alpha1.Hook{
			Webhook: &v1alpha1.Webhook{
				URL: &customizeHookUrl,
			},
		}
	} else {
		customizeHook = nil
	}

	cc := &v1alpha1.CompositeController{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
		},
		Spec: v1alpha1.CompositeControllerSpec{
			// Set a big resyncPeriod so tests can precisely control when syncs happen.
			ResyncPeriodSeconds: pointer.Int32Ptr(3600),
			ParentResource: v1alpha1.CompositeControllerParentResourceRule{
				ResourceRule:  *parentRule,
				LabelSelector: nil,
			},
			ChildResources: childResources,
			Hooks: &v1alpha1.CompositeControllerHooks{
				Sync: &v1alpha1.Hook{
					Webhook: &v1alpha1.Webhook{
						URL: &syncHookURL,
					},
				},
				Customize: customizeHook,
			},
		},
	}

	// Add labels if specified.
	if labels != nil {
		cc.ObjectMeta.Labels = *labels
	}

	return cc
}
