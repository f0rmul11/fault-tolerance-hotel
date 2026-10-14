package kube

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRenderKeepsUnreadyAdminAccessAndSetsImmutableImage(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	objects, err := Render(root, cfg.Services[0], "hotelflow/api-gateway:sha-test")
	if err != nil {
		t.Fatal(err)
	}
	foundAdmin, foundDeployment := false, false
	for _, obj := range objects {
		if obj.GetKind() == "Service" && obj.GetName() == "api-gateway-admin" {
			allowed, _, _ := unstructured.NestedBool(obj.Object, "spec", "publishNotReadyAddresses")
			if !allowed {
				t.Fatal("admin would disappear when readiness fails")
			}
			foundAdmin = true
		}
		if obj.GetKind() == "Deployment" {
			containers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
			if containers[0].(map[string]any)["image"] != "hotelflow/api-gateway:sha-test" {
				t.Fatal("image not replaced")
			}
			foundDeployment = true
		}
	}
	if !foundAdmin || !foundDeployment {
		t.Fatal("missing resources")
	}
}

func TestOldReadyReplicaDoesNotCompleteNewRollout(t *testing.T) {
	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "gateway",
			Namespace:  "hotel",
			Generation: 2,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
			Replicas:           1,
		},
	}
	client := &Client{
		Typed: fake.NewSimpleClientset(deployment),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := client.WaitDeployment(ctx, config.Service{
		Name:       "gateway",
		Namespace:  "hotel",
		Deployment: "gateway",
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("old replica incorrectly completed rollout: %v", err)
	}
}

func TestAllFoundationServicesKeepAdminAndGRPCPorts(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range cfg.Services[1:] {
		t.Run(service.Name, func(t *testing.T) {
			// Explicit override must affect manifests as well as Docker mappings.
			service.GRPC.Node = 31091
			objects, err := Render(root, service, service.Image+":sha-test")
			if err != nil {
				t.Fatal(err)
			}
			foundGRPC, foundAdmin := false, false
			for _, object := range objects {
				if object.GetKind() != "Service" {
					continue
				}
				ports, _, _ := unstructured.NestedSlice(object.Object, "spec", "ports")
				if object.GetName() == service.Name {
					if len(ports) != 1 || ports[0].(map[string]any)["nodePort"] != int64(31091) {
						t.Fatal("gRPC NodePort override not applied")
					}
					foundGRPC = true
				}
				if object.GetName() == service.Name+"-admin" {
					allowed, _, _ := unstructured.NestedBool(object.Object, "spec", "publishNotReadyAddresses")
					if !allowed || len(ports) != 1 || ports[0].(map[string]any)["nodePort"] != int64(service.Admin.Node) {
						t.Fatal("admin access lost")
					}
					foundAdmin = true
				}
			}
			if !foundGRPC || !foundAdmin {
				t.Fatal("missing service endpoints")
			}
		})
	}
}
