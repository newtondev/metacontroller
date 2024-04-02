/*
 *
 * Copyright 2024. Metacontroller authors.
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * https://www.apache.org/licenses/LICENSE-2.0
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 * /
 */

package funcs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	metacontrollerv1alpha1 "metacontroller/pkg/apis/metacontroller/v1alpha1"
)

type kindConfigContextKey string

// HelmRepo manages a Helm repo.
func HelmRepo(o ...helm.Option) env.Func {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		err := helm.New(c.KubeconfigFile()).RunRepo(o...)
		return ctx, errors.Wrap(err, "cannot install Helm chart")
	}
}

// HelmInstall installs a Helm chart.
func HelmInstall(o ...helm.Option) env.Func {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		err := helm.New(c.KubeconfigFile()).RunInstall(o...)
		return ctx, errors.Wrap(err, "cannot install Helm chart")
	}
}

// HelmUpgrade upgrades a Helm chart.
func HelmUpgrade(o ...helm.Option) env.Func {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		err := helm.New(c.KubeconfigFile()).RunUpgrade(o...)
		return ctx, errors.Wrap(err, "cannot upgrade Helm chart")
	}
}

// AsFeaturesFunc converts an env.Func to a features.Func. If the env.Func
// returns an error the calling test is failed with t.Fatal(err).
func AsFeaturesFunc(fn env.Func) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		t.Helper()

		ctx, err := fn(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		return ctx
	}
}

// HelmUninstall uninstalls a Helm chart.
func HelmUninstall(o ...helm.Option) env.Func {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		err := helm.New(c.KubeconfigFile()).RunUninstall(o...)
		return ctx, errors.Wrap(err, "cannot uninstall Helm chart")
	}
}

// AddMetacontrollerTypesToScheme adds Metacontroller's core custom resources to the
// environments scheme. This allows the environments client to work with said types.
func AddMetacontrollerTypesToScheme() env.Func {
	return func(ctx context.Context, c *envconfig.Config) (context.Context, error) {
		_ = metacontrollerv1alpha1.AddToScheme(c.Client().Resources().GetScheme())
		return ctx, nil
	}
}

// EnvFuncs runs the supplied functions in order, returning the first error.
func EnvFuncs(fns ...env.Func) env.Func {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		for _, fn := range fns {
			var err error
			ctx, err = fn(ctx, c)
			if err != nil {
				return ctx, err
			}
		}
		return ctx, nil
	}
}

// CreateKindClusterWithConfig create kind cluster of the given name according to
// configuration referred via configFilePath.
// The configuration is placed in test context afterward.
func CreateKindClusterWithConfig(clusterName, configFilePath string) env.Func {
	return EnvFuncs(
		envfuncs.CreateKindClusterWithConfig(kind.NewProvider(), clusterName, configFilePath),
		func(ctx context.Context, _ *envconf.Config) (context.Context, error) {
			b, err := os.ReadFile(filepath.Clean(configFilePath))
			if err != nil {
				return ctx, err
			}
			cfg := &v1alhpa4.Cluster{}
			err = yaml.Unmarshal(b, cfg)
			if err != nil {
				return ctx, err
			}
			return context.WithValue(ctx, kindConfigContextKey(clusterName), cfg), nil
		},
	)
}

// ServiceIngressEndpoint returns endpoint (addr:port) that can be used for accessing
// the service in the cluster with the given name.
func ServiceIngressEndpoint(ctx context.Context, cfg *envconf.Config, clusterName, namespace, serviceName string) (string, error) {
	_, found := envfuncs.GetClusterFromContext(ctx, clusterName)
	client := cfg.Client()
	service := &corev1.Service{}
	err := client.Resources().Get(ctx, serviceName, namespace, service)
	if err != nil {
		return "", errors.Errorf("cannot get service %s/%s at cluster %s: %w", namespace, serviceName, clusterName, err)
	}

	var nodePort int32
	for _, p := range service.Spec.Ports {
		if p.NodePort != 0 {
			nodePort = p.NodePort
			break
		}
	}
	if nodePort == 0 {
		return "", errors.Errorf("nso nodePort found for service %s/%s at cluster %s", namespace, serviceName, clusterName)
	}
	if found {
		kinCfg, err := kindConfig(ctx, clusterName)
		if err != nil {
			return "", errors.Errorf("cannot get kind config for cluster %s: %w", clusterName, err)
		}
		hostPort, err := findHostPort(kindCfg, nodePort)
		if err != nil {
			return "", errors.Errorf("cannot find hostPort for nodePort %d in kind config for cluster %s: %w", nodePort, clusterName, err)
		}
		return fmt.Sprintf("localhost:%v", hostPort), nil
	}
	nodes := &corev1.NodeList{}
	if err := client.Resources().List(ctx, nodes); err != nil {
		return "", errors.Errorf("cannot list nodes for cluster %s: %w", clusterName, err)
	}
	addr, err := findAnyNodeIPAddress(nodes)
	if err != nil {
		return "", errors.Errorf("cannot find any node IP address for cluster %s: %w", clusterName, err)
	}
	return fmt.Sprintf("%s:%v", addr, nodePort), nil
}

func kindConfig(ctx context.Context, clusterName string) (*v1alpha4.Cluster, error) {
	v := ctx.Value(kindConfigContextKey(clusterName))
	if v == nil {
		return nil, errors.Errorf("no kind config found in context for cluster %s", clusterName)
	}
	kindCfg, ok := v.(*v1alpha4.Cluster)
	if !ok {
		return nil, errors.Errorf("kind config is not of type v1alpha4.Cluster for clustername %s", clusterName)
	}
	return kindCfg, nil
}

func findAnyNodeIPAddress(nodes *corev1.NodeList) (string, error) {
	if len(nodes.Items) == 0 {
		return "", errors.New("no nodes in the cluster")
	}
	for _, a := range nodes.Items[0].Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			return a.Address, nil
		}
	}
	return "", errors.Errorf("no ip address found for nodes: %v", nodes)
}

func findHostPort(kindCfg *v1alhpa4.Cluster, containerPort int32) (int32, error) {
	for _, n := range kindCfg.Nodes {
		if n.Role == v1alpha4.ControlPlaneRole {
			for _, pm := range n.ExtraPortMappings {
				if pm.ContainerPort == containerPort {
					return pm.HostPort, nil
				}
			}
		}
	}
	return 0, errors.Errorf("no host port found in kind config for container port: %v", containerPort)
}
