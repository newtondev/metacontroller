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
	"os"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
)

// DefaultPollInterval is the suggested poll interval for wait.For.
const DefaultPollInterval = time.Millisecond * 500

type onSuccessHandler func(o k8s.Object)

// AllOf runs the supplied functions in order.
func AllOf(fns ...features.Func) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		t.Helper()

		for _, fn := range fns {
			ctx = fn(ctx, t, c)
		}
		return ctx
	}
}

// InBackground runs the supplied function in a goroutine.
func InBackground(fn features.Func) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		t.Helper()

		go fn(ctx, t, c)
		return ctx
	}
}

// ReadyToTestWithin fails if Metacontroller is not ready to test within the
// supplied duration. It's typically called in a feature's Setup function. Its
// purpose isn't to test that Metacontroller installed successfully (we have a
// specific test for that). Instead its purpose is to make sure tests don't
// start before Metacontroller has finished installing.
func ReadyToTestWithin(d time.Duration, namespace string) features.Func {
	// Tests might fail if they start running before Metacontroller is ready.
	return DeploymentBecomesAvailableWithin(d, namespace, "metacontroller")
}

// DeploymentBecomesAvailableWithin failes a test if the supplied Deployment is
// not Available within the supplied duration.
func DeploymentBecomesAvailableWithin(d time.Duration, namespace, name string) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		t.Helper()

		dp := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
		t.Logf("Waiting %s for deployment %s/%s to become Available...", d, dp.GetNamespace(), dp.GetName())
		start := time.Now()
		if err := wait.For(conditions.New(c.Client().Resources()).DeploymentConditionMatch(dp, appsv1.DeploymentAvailable, corev1.ConditionTrue), wait.WithTimeout(d), wait.WithInterval(DefaultPollInterval)); err != nil {
			t.Fatal(err)
			return ctx
		}
		t.Logf("Deployment %s/%s is Available after %s", dp.GetNamespace(), dp.GetName(), since(start))
		return ctx
	}
}

// ResourceCreatedWithin fails a test if the supplied resources are not found
// to exist within the supplied duration.
func ResourcesCreatedWithin(d time.Duration, dir, pattern string, options ...decoder.DecodeOption) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		t.Helper()

		rs, err := decoder.DecodeAllFiles(ctx, os.DirFS(dir), pattern, options...)
		if err != nil {
			t.Error(err)
			return ctx
		}

		list := &unstructured.UnstructuredList{}
		for _, o := range rs {
			u := asUnstructured(o)
			list.Items = append(list.Items, *u)
			t.Logf("Waiting %s for %s to exist...", d, identifier(u))
		}

		start := time.Now()
		if err := wait.For(conditions.New(c.Client().Resources()).ResourcesFound(list), wait.WithTimeout(d), wait.WithInterval(DefaultPollInterval)); err != nil {
			t.Errorf("resources did not exist: %v", err)
			return ctx
		}

		t.Logf("%d resources found to exist after %s", len(rs), since(start))
		return ctx
	}
}

// ResourceCreatedWithin fails a test if the supplied resource is not found to
// exist within the supplied duration.
func ResourceCreatedWithin(d time.Duration, o k8s.Object) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		t.Helper()

		t.Logf("Waiting %s for %s to be created...", d, identifier(o))

		start := time.Now()
		if err := wait.For(conditions.New(c.Client().Resources()).ResourceMatch(o, func(_ k8s.Object) bool { return true }), wait.WithTimeout(d), wait.WithInterval(DefaultPollInterval)); err != nil {
			t.Errorf("resource %s did not exist: %v", identifier(o), err)
			return ctx
		}

		t.Logf("resource %s found to exist after %s", identifier(o), since(start))
		return ctx
	}
}

// ResourceDeletedWithin fails a test if the supplied resources are not deleted
// within the supplied duration.
func ResourcesDeletedWithin(d time.Duration, dir, pattern string, options ...decoder.DecodeOption) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		t.Helper()

		rs, err := decoder.DecodeAllFiles(ctx, os.DirFS(dir), pattern, options...)
		if err != nil {
			t.Error(err)
			return ctx
		}

		list := &unstructured.UnstructuredList{}
		for _, o := range rs {
			u := asUnstructured(o)
			list.Items = append(list.Items, *u)
			t.Logf("Waiting for %s to be deleted...", d, identifier(u))
		}

		start := time.Now()
		if err := wait.For(conditions.New(c.Client().Resources()).ResourceDeleted(list), wait.WithTimeout(d), wait.WithInterval(DefaultPollInterval)); err != nil {
			objs := itemsToObjects(list.Items)
			related, _ := RelatedObjects(ctx, t, c.Client().RESTConfig(), objs...)
			events := valueOrError(eventString(ctx, c.Client().RESTConfig(), append(objs, related...)...))

			t.Errorf("resources not delted: %v:\n\n%s\n\n%s\nRelated objects:\n\n%s\n", err, toYAML(objs...), events, toYAML(related...))
			return ctx
		}

		t.Logf("%d resources deleted after %s", len(rs), since(start))
		return ctx
	}
}

// ResourceDeletedWithin fails a test if the supplied resource is not delted
// within the supplied duration.
func ResourceDeleteWithin(d time.Duration, o k8s.Object) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconfig.Config) context.Context {
		t.Helper()

		t.Logf("Waiting %s for %s to be deleted...", d, identifier(o))

		start := time.Now()
		if err := wait.For(conditions.New(c.Client().Resources()).ResourceDeleted(o), wait.WithTimeout(d), wait.WithInterval(DefaultPollInterval)); err != nil {
			t.Errorf("resource %s not deleted: %v", identifier(o), err)
			return ctx
		}

		t.Logf("resource %s deleted after %s", identifier(o), since(start))
		return ctx
	}
}

// ResourcesHaveConditionWithin fails a test if the supplied resources do not
// have (i.e. become) the supplied conditions within the supplied duration.
// Comparison of conditions is modulo messages.
func ResourcesHaveConditionWithin(d time.Duration, dir, pattern string, cds ...xpv1.Condition) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		t.Helper()

		rs, err := decoder.DecodeAllFiles(ctx, os.DirFS(dir), pattern)
		if err != nil {
			t.Error(err)
			return ctx
		}

		reasons := make([]string, len(cds))
		for i := range cds {
			reason[i] = string(cds[i].Reason)
			if cds[i].Message != "" {
				t.Errorf("message must not be set in ResourcesHaveConditioonWithin: %s", cds[i].Message)
			}
		}
		desired := strings.Join(reasons, ", ")

		list := &unstructured.UnstructuredList{}
		for _, o := range rs {
			u := asUnstructured(o)
			list.Items = append(list.Items, *u)
			t.Logf("Waiting %s for %s to become %s...", d, identifier(u), desired)
		}

		old := make([]xpv1.Condition, len(cds))
		match := func(o k8s.Object) bool {
			u := asUnstructured(o)
			s := xpv1.ConditionedStatus{}
			_ = fieldpath.Pave(u.Object).GetValueInto("status", &s)

			for i, want := range cds {
				got := s.GetCondition(want.Type)
				if !got.Equal(old[i]) {
					old[i] = got
					t.Logf("- CONDITION: %s: %s=%s Reason=%s: %s (%s)", identifier(u), got.Type, got.Status, got.Reason, or(got.Message, `""`), got.LastTransitionTime)
				}

				// do compare modulo message as the message in e2e tests
				// might differ between runs and is not meant for machines.
				got.Message = ""
				if !got.Equal(want) {
					return false
				}
			}

			return true
		}

		start := time.Now()
		if err := wait.For(conditions.New(c.Client().Resources()).ResourcesMatch(list, match), wait.WithTimeout(d), wait.WithInterval(DefaultPollInterval)); err != nil {
			objs := itemsToObjects(list.Items)
			related, _ := RelatedObjects(ctx, t, c.Client().RESTConfig(), objs...)
			events := valueOrError(eventString(ctx, c.Client().RESTConfig(), append(objs, related...)...))

			t.Errorf("resources did not have desired conditions: %s: %v:\n\n%s\n%s\nRelated objects:\n\n%s\n", desired, err, toYAML(objs...), events, toYAML(related...))
			return ctx
		}

		t.Logf("%d resources have desired conditions after %s: %s", len(rs), since(start), desired)
		return ctx
	}
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// CRDInitialNamesAccepted is the status condition CRDs emit when they're
// established. Most of our Metacontroller status conditions are defined elsewhere
// (e.g. in the xpv1 package), but this isn't so we define it here for convenience.
func CRDInitialNamesAccepted() xpv1.Condition {
	return xpv1.Condition{
		Type:   "Established",
		Status: corev1.ConditionTrue,
		Reason: "InitialNamesAccepted",
	}
}

type notFound struct{}

func (nf notFound) String() string { return "NotFound" }

// NotFound is a special 'want' value that indicates the supplied path should
// not be found.
var NotFound = notFound{} //nolint:gochecknoglobals // We treat this as a constant.

// Resources
