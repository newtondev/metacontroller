/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"metacontroller/pkg/apis/metacontroller/v1alpha1"
	dynamicdiscovery "metacontroller/pkg/dynamic/discovery"
	"metacontroller/pkg/options"
	"metacontroller/pkg/server"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

const (
	// APIGroup is the group used for CRDs created as part of the test.
	APIGroup = "test.metacontroller"
	// APIVersion is the group-version used for CRDs created as part of the test.
	APIVersion = APIGroup + "/v1"
)

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var ctx context.Context
var cancel context.CancelFunc
var resourceMap *dynamicdiscovery.ResourceMap

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "manifests", "production")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = v1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	configuration := defaultConfiguration()
	configuration.RestConfig = cfg

	// Setup the manager
	mgr, err := server.New(configuration)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err := mgr.Start(ctx) // start the manager
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

	// Periodically refresh discovery to pick up newly-installed resources.
	discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(cfg)
	resourceMap = dynamicdiscovery.NewResourceMap(discoveryClient)
	// We don't care about stopping this cleanly since it has no external effects.
	resourceMap.Start(500 * time.Millisecond)
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := (func() (err error) {
		// Need to sleep if the first stop fails due to a bug:
		// https://github.com/kubernetes-sigs/controller-runtime/issues/1571
		sleepTime := 1 * time.Millisecond
		for i := 0; i < 12; i++ { // Exponentially sleep up to ~4s
			if err = testEnv.Stop(); err == nil {
				return
			}
			sleepTime *= 2
			time.Sleep(sleepTime)
		}
		return
	})()
	Expect(err).NotTo(HaveOccurred())
})

func ComposeCRD(kind string, scope apiextensionsv1.ResourceScope) *apiextensionsv1.CustomResourceDefinition {
	return composeCRD(kind, scope, true)
}

func composeCRD(kind string, scope apiextensionsv1.ResourceScope, withSubresourceStatus bool) *apiextensionsv1.CustomResourceDefinition {
	singular := strings.ToLower(kind)
	plural := singular + "s"
	xPreserveUnknownFields := true
	var subresources *apiextensionsv1.CustomResourceSubresources
	if withSubresourceStatus {
		subresources = &apiextensionsv1.CustomResourceSubresources{
			Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
		}
	} else {
		subresources = nil
	}
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s.%s", plural, APIGroup),
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: APIGroup,
			Scope: scope,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Singular: singular,
				Plural:   plural,
				Kind:     kind,
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							XPreserveUnknownFields: &xPreserveUnknownFields,
						},
					},
					Subresources: subresources,
				},
			},
		},
	}
}

// ServeWebhook starts an HTTP server to handle webhook requests.
// Ensure that you defer the call srv.Close() to shut down the server.
func ServeWebhook(handler func(request []byte) (response []byte, err error)) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			http.Error(w, "can't read body", http.StatusBadRequest)
			return
		}

		resp, err := handler(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(resp)
	}))
	return srv
}

// ComposeNamespace composes a Kubernetes namespace object.
func ComposeNamespace(namespace string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
}

// UnstructuredCRD creates a new Unstructured object for the given CRD.
func UnstructuredCRD(crd *apiextensionsv1.CustomResourceDefinition, name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(crd.Spec.Group + "/" + crd.Spec.Versions[0].Name)
	obj.SetKind(crd.Spec.Names.Kind)
	obj.SetName(name)
	return obj
}

// UnstructuredJSON creates a new Unstructured object from the given JSON.
// It panics on a decode error because it's meant for use with hard-coded test
// data.
func UnstructuredJSON(apiVersion, kind, name, jsonStr string) *unstructured.Unstructured {
	obj := map[string]interface{}{}
	if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
		panic(err)
	}
	u := &unstructured.Unstructured{Object: obj}
	u.SetAPIVersion(apiVersion)
	u.SetKind(kind)
	u.SetName(name)
	return u
}

func CRDResourceRule(crd *apiextensionsv1.CustomResourceDefinition) *v1alpha1.ResourceRule {
	return &v1alpha1.ResourceRule{
		APIVersion: crd.Spec.Group + "/" + crd.Spec.Versions[0].Name,
		Resource:   crd.Spec.Names.Plural,
	}
}

func defaultConfiguration() options.Configuration {
	return options.Configuration{
		DiscoveryInterval: 500 * time.Millisecond,
		InformerRelist:    30 * time.Minute,
		Workers:           5,
		CorrelatorOptions: record.CorrelatorOptions{},
	}
}
