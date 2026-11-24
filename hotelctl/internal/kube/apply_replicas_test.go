package kube

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestApplyResourcePreservesExternallyScaledReplicas(t *testing.T) {
	replicas := metav1.StatusCause{
		Type:  metav1.CauseTypeFieldManagerConflict,
		Field: ".spec.replicas",
	}
	image := metav1.StatusCause{
		Type:  metav1.CauseTypeFieldManagerConflict,
		Field: ".spec.template.spec.containers[name=\"notification\"].image",
	}
	tests := []struct {
		name   string
		causes []metav1.StatusCause
		retry  bool
	}{
		{name: "manifest applies without conflict"},
		{name: "replicas changed by scale", causes: []metav1.StatusCause{replicas}, retry: true},
		{name: "replicas and image conflict", causes: []metav1.StatusCause{replicas, image}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"spec": map[string]any{
					"replicas": int64(1),
					"template": map[string]any{"metadata": map[string]any{}},
				},
			}}
			object.SetName("notification")
			object.SetNamespace("hotelflow")

			var conflict error
			if len(tt.causes) != 0 {
				conflict = &apierrors.StatusError{ErrStatus: metav1.Status{
					Reason: metav1.StatusReasonConflict,
					Code:   409,
					Details: &metav1.StatusDetails{
						Causes: tt.causes,
					},
				}}
			}

			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			calls := 0
			client.PrependReactor("patch", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
				calls++
				var body map[string]any
				if err := json.Unmarshal(action.(k8stesting.PatchAction).GetPatch(), &body); err != nil {
					t.Fatal(err)
				}
				_, hasReplicas, _ := unstructured.NestedFieldNoCopy(body, "spec", "replicas")
				if hasReplicas != (calls == 1) {
					t.Fatalf("patch %d replicas present = %t", calls, hasReplicas)
				}
				if _, found, _ := unstructured.NestedMap(body, "spec", "template"); !found {
					t.Fatal("pod template missing from patch")
				}
				if calls == 1 && conflict != nil {
					return true, nil, conflict
				}
				return true, object, nil
			})

			resource := client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace("hotelflow")
			err := applyResource(context.Background(), resource, object)
			if tt.retry {
				if err != nil || calls != 2 {
					t.Fatalf("retry: calls=%d err=%v", calls, err)
				}
			} else if !errors.Is(err, conflict) || calls != 1 {
				t.Fatalf("no retry: calls=%d err=%v", calls, err)
			}
			if count, _, _ := unstructured.NestedInt64(object.Object, "spec", "replicas"); count != 1 {
				t.Fatal("rendered manifest was modified")
			}
		})
	}
}
