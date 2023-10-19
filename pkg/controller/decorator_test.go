package controller

import (
	"metacontroller/pkg/apis/metacontroller/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	//. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var _ = Describe("DecoratorController controller", func() {

})

func createDecoratorController(name, syncHookURL, customizeHookUrl string, parentRule, childRule *v1alpha1.ResourceRule, labels *map[string]string) *v1alpha1.DecoratorController {
	childResources := []v1alpha1.DecoratorControllerAttachmentRule{}
	if childRule != nil {
		childResources = append(childResources, v1alpha1.DecoratorControllerAttachmentRule{ResourceRule: *childRule})
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

	dc := &v1alpha1.DecoratorController{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
		},
		Spec: v1alpha1.DecoratorControllerSpec{
			// Set a big resyncPeriod so tests can precisely control when syncs happen.
			ResyncPeriodSeconds: pointer.Int32(3600),
			Resources: []v1alpha1.DecoratorControllerResourceRule{
				{
					ResourceRule: *parentRule,
				},
			},
			Attachments: childResources,
			Hooks: &v1alpha1.DecoratorControllerHooks{
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
		dc.ObjectMeta.Labels = *labels
	}

	return dc
}
