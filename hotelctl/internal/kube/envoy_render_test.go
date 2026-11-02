package kube

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRenderEnvoyBootstrap(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	objects, err := RenderEnvoy(config.Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range objects {
		if object.GetKind() != "ConfigMap" || object.GetName() != "envoy-config" {
			continue
		}
		bootstrap, found, err := unstructured.NestedString(object.Object, "data", "envoy.json")
		if err != nil || !found || !json.Valid([]byte(bootstrap)) {
			t.Fatalf("invalid Envoy bootstrap: found=%v err=%v", found, err)
		}
		return
	}
	t.Fatal("Envoy ConfigMap missing")
}
