package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

// Render uses the same Kustomize library as kubectl, without rewriting manifests on disk.
func Render(root string, s config.Service, image string) ([]*unstructured.Unstructured, error) {
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	resources, err := k.Run(filesys.MakeFsOnDisk(), filepath.Join(root, s.Manifests))
	if err != nil {
		return nil, fmt.Errorf("render %s: %w", s.Name, err)
	}
	data, err := resources.AsYaml()
	if err != nil {
		return nil, err
	}
	decoder := k8syaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var objects []*unstructured.Unstructured
	for {
		object := &unstructured.Unstructured{}
		if err := decoder.Decode(&object.Object); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		if len(object.Object) == 0 {
			continue
		}
		replaceImage(object.Object, s.Image, image)
		configureNodePorts(object, s)
		objects = append(objects, object)
	}
	return objects, nil
}

func replaceImage(value any, repository, image string) {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if key == "image" {
				if name, ok := item.(string); ok && (name == repository || strings.HasPrefix(name, repository+":") || strings.HasPrefix(name, repository+"@")) {
					v[key] = image
				}
			}
			replaceImage(item, repository, image)
		}
	case []any:
		for _, item := range v {
			replaceImage(item, repository, image)
		}
	}
}

func (c *Client) Apply(ctx context.Context, objects []*unstructured.Unstructured, out io.Writer) error {
	dc, err := discovery.NewDiscoveryClientForConfig(c.REST)
	if err != nil {
		return err
	}
	resources, err := restmapper.GetAPIGroupResources(dc)
	if err != nil {
		return err
	}
	mapper := restmapper.NewDiscoveryRESTMapper(resources)
	for _, object := range objects {
		mapping, err := mapper.RESTMapping(object.GroupVersionKind().GroupKind(), object.GroupVersionKind().Version)
		if err != nil {
			return err
		}
		data, err := json.Marshal(object.Object)
		if err != nil {
			return err
		}
		resource := c.Dynamic.Resource(mapping.Resource)
		options := metav1.PatchOptions{FieldManager: "hotelctl", FieldValidation: "Strict"}
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			_, err = resource.Namespace(object.GetNamespace()).Patch(ctx, object.GetName(), types.ApplyPatchType, data, options)
		} else {
			_, err = resource.Patch(ctx, object.GetName(), types.ApplyPatchType, data, options)
		}
		if err != nil {
			return fmt.Errorf("apply %s/%s: %w", object.GetKind(), object.GetName(), err)
		}
		fmt.Fprintf(out, "%s/%s applied\n", strings.ToLower(object.GetKind()), object.GetName())
	}
	return nil
}

func configureNodePorts(object *unstructured.Unstructured, s config.Service) {
	if object.GetKind() != "Service" {
		return
	}
	kind, _, _ := unstructured.NestedString(object.Object, "spec", "type")
	if kind != "NodePort" {
		return
	}
	ports, found, _ := unstructured.NestedSlice(object.Object, "spec", "ports")
	if !found {
		return
	}
	for _, raw := range ports {
		port, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if object.GetName() == s.Name && port["name"] == "grpc" && s.GRPC.Node != 0 {
			port["nodePort"] = int64(s.GRPC.Node)
		}
		if object.GetName() == s.Name+"-admin" && port["name"] == "admin" && s.Admin.Node != 0 {
			port["nodePort"] = int64(s.Admin.Node)
		}
	}
	_ = unstructured.SetNestedSlice(object.Object, ports, "spec", "ports")
}
