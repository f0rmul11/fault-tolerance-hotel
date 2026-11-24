package kube

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestLogsAllIncludesOlderReplicas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/apps/v1/namespaces/hotelflow/deployments/notification":
			fmt.Fprint(w, `{"apiVersion":"apps/v1","kind":"Deployment","spec":{"selector":{"matchLabels":{"app":"notification"}}}}`)
		case "/api/v1/namespaces/hotelflow/pods":
			if r.URL.Query().Get("labelSelector") != "app=notification" {
				t.Error("logs query lost deployment selector")
			}
			fmt.Fprint(w, `{"apiVersion":"v1","kind":"PodList","items":[{"metadata":{"name":"older","creationTimestamp":"2026-11-24T10:00:00Z"}},{"metadata":{"name":"newer","creationTimestamp":"2026-11-24T11:00:00Z"}}]}`)
		case "/api/v1/namespaces/hotelflow/pods/older/log":
			fmt.Fprintln(w, `{"msg":"older replica handled the event"}`)
		case "/api/v1/namespaces/hotelflow/pods/newer/log":
			fmt.Fprintln(w, `{"msg":"newer replica is idle"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	typed, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Typed: typed}
	service := config.Service{Name: "notification", Namespace: "hotelflow", Deployment: "notification"}
	var output bytes.Buffer
	if err := client.LogsAll(t.Context(), service, 1000, &output); err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{"older replica handled the event", "newer replica is idle"} {
		if !strings.Contains(output.String(), message) {
			t.Fatalf("replica log missing: %s", output.String())
		}
	}

	// Интерактивная команда logs по-прежнему выбирает новейший Pod.
	output.Reset()
	if err := client.Logs(t.Context(), service, false, 1000, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "older replica") || !strings.Contains(output.String(), "newer replica") {
		t.Fatalf("single-pod logs changed: %s", output.String())
	}
}
