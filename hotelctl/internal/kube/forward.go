package kube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/fault"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type Forward struct {
	Ports map[int]int
	stop  chan struct{}
	once  sync.Once
}

func (c *Client) CheckEnvoyRoute(ctx context.Context, source config.Service, edge fault.Edge) error {
	d, err := c.Typed.AppsV1().Deployments(source.Namespace).Get(ctx, source.Deployment, metav1.GetOptions{})
	if err != nil {
		return err
	}
	expected := fmt.Sprintf("%s:envoy.hotelflow.svc.cluster.local:%d", edge.To, edge.Port)
	for _, container := range d.Spec.Template.Spec.Containers {
		if container.Name != source.Name {
			continue
		}
		for _, env := range container.Env {
			if env.Name == "SERVICE_TARGETS" {
				for _, target := range strings.Split(env.Value, ",") {
					if target == expected {
						return nil
					}
				}
			}
		}
	}
	return fmt.Errorf("%s → %s ещё не направлен через Envoy; выполните hotelctl up", edge.From, edge.To)
}

func (f *Forward) Close() {
	f.once.Do(func() {
		close(f.stop)
	})
}

// ForwardEnvoy pins both control and data checks to the same ready Envoy Pod.
// The returned tunnel outlives ctx cancellation so cleanup can still use it.
func (c *Client) ForwardEnvoy(ctx context.Context, ports []int, errOut io.Writer) (*Forward, error) {
	pods, err := c.Typed.CoreV1().Pods("hotelflow").List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=envoy",
	})
	if err != nil {
		return nil, err
	}
	var names []string
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				names = append(names, pod.Name)
			}
		}
	}
	if len(names) != 1 {
		return nil, fmt.Errorf("нужен ровно один готовый Pod Envoy, найдено %d; выполните hotelctl up", len(names))
	}
	cfg := rest.CopyConfig(c.REST)
	cfg.Timeout = 0
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, err
	}
	endpoint := c.Typed.CoreV1().RESTClient().Post().Namespace("hotelflow").Resource("pods").Name(names[0]).SubResource("portforward").URL()
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, endpoint)
	requested := make([]string, 0, len(ports))
	for _, port := range ports {
		requested = append(requested, "0:"+strconv.Itoa(port))
	}
	forward := &Forward{
		Ports: make(map[int]int),
		stop:  make(chan struct{}),
	}
	ready := make(chan struct{})
	pf, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, requested, forward.stop, ready, io.Discard, errOut)
	if err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		done <- pf.ForwardPorts()
	}()
	select {
	case <-ctx.Done():
		forward.Close()
		return nil, ctx.Err()
	case err := <-done:
		forward.Close()
		return nil, fmt.Errorf("port-forward Envoy завершился: %v", err)
	case <-ready:
	}
	bound, err := pf.GetPorts()
	if err != nil {
		forward.Close()
		return nil, err
	}
	for _, port := range bound {
		forward.Ports[int(port.Remote)] = int(port.Local)
	}
	return forward, nil
}
