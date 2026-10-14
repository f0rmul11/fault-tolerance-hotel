package healthcheck

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPanickingCheckReturnsUnavailable(t *testing.T) {
	h := NewHandler()
	h.AddReadinessCheck("dependency", func() error {
		panic("unavailable")
	})
	for i := 0; i < 100; i++ {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, ReadinessPath, nil))
		if response.Code != http.StatusServiceUnavailable ||
			!strings.Contains(response.Body.String(), "panic recovered") {
			t.Fatalf("panic result: %d %s", response.Code, response.Body.String())
		}
	}
}
