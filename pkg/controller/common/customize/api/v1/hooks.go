package v1

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"metacontroller/pkg/apis/metacontroller/v1alpha1"
	"metacontroller/pkg/controller/common/api"
)

// CustomizeHookRequest is a request send to customize hook
type CustomizeHookRequest struct {
	Controller v1alpha1.CustomizableController `json:"controller"`
	Parent     *unstructured.Unstructured      `json:"parent"`
}

func (r *CustomizeHookRequest) GetParent() *unstructured.Unstructured {
	return r.Parent
}

func (r *CustomizeHookRequest) GetChildren() api.ObjectMap {
	return nil
}

func (r *CustomizeHookRequest) GetRelated() api.ObjectMap {
	return nil
}

func (r *CustomizeHookRequest) IsFinalizing() bool {
	return false
}

// CustomizeHookResponse is a response from customize hook
type CustomizeHookResponse struct {
	RelatedResourceRules []*v1alpha1.RelatedResourceRule `json:"relatedResources,omitempty"`
}
