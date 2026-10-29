package kube

import (
	"context"
	"fmt"
	"io"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// InstallCanaryInfrastructure applies only local, version-pinned manifests.
// Take ownership of these pinned infrastructure fields from the previous istioctl
// installer. Application Rollouts and traffic weights never use forced apply.
// CRDs must be established before discovery is refreshed for application resources.
func (c *Client) InstallCanaryInfrastructure(ctx context.Context, cfg config.Config, out io.Writer) error {
	for _, component := range []struct{ path, namespace, deployment string }{
		{path: "deploy/istio", namespace: "istio-system", deployment: "istiod"},
		{path: "deploy/argo-rollouts", namespace: "argo-rollouts", deployment: "argo-rollouts"},
	} {
		crds, err := Render(cfg.Root, config.Service{Manifests: component.path + "/crds"}, "")
		if err != nil {
			return err
		}
		controller, err := Render(cfg.Root, config.Service{Manifests: component.path + "/controller"}, "")
		if err != nil {
			return err
		}
		if err := c.apply(ctx, crds, out, true); err != nil {
			return err
		}
		for _, object := range crds {
			if object.GetKind() != "CustomResourceDefinition" {
				continue
			}
			name := object.GetName()
			if err := Poll(ctx, "регистрация CRD "+name, func(ctx context.Context) (bool, error) {
				current, err := c.Dynamic.Resource(schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}).Get(ctx, name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				conditions, _, _ := unstructured.NestedSlice(current.Object, "status", "conditions")
				for _, raw := range conditions {
					condition, ok := raw.(map[string]any)
					if ok && condition["type"] == "Established" && condition["status"] == "True" {
						return true, nil
					}
				}
				return false, nil
			}); err != nil {
				return err
			}
		}
		if err := c.apply(ctx, controller, out, true); err != nil {
			return err
		}
		fmt.Fprintf(out, "Ожидание %s\n", component.deployment)
		if err := c.WaitDeployment(ctx, config.Service{Name: component.deployment, Deployment: component.deployment, Namespace: component.namespace}); err != nil {
			return err
		}
	}
	return nil
}
