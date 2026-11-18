package kafkafault

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTCPFaultAndCleanup(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body map[string]any
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		switch {
		case requests <= 3:
			if body["type"] != "timeout" || body["stream"] != "downstream" || body["attributes"].(map[string]any)["timeout"] != float64(0) {
				t.Errorf("expected indefinitely blocked responses: %v", body)
			}
		case requests <= 6:
			if body["enabled"] != false {
				t.Errorf("expected closed connections: %v", body)
			}
		default:
			if body["enabled"] != true {
				t.Errorf("expected restored connections: %v", body)
			}
		}
	}))
	defer server.Close()
	client := NewClient(server.URL)
	defer client.Close()
	ctx := context.Background()
	if err := client.BlockResponses(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if requests != 12 {
		t.Fatalf("requests=%d", requests)
	}
}
