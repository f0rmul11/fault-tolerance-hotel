package hedge

import (
	"fmt"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHandlerEnabledOverridesDefault(t *testing.T) {
	tests := []struct {
		name           string
		defaultEnabled bool
		handlerEnabled string
		want           bool
	}{
		{
			name:           "explicit false overrides true",
			defaultEnabled: true,
			handlerEnabled: "enabled: false",
		},
		{
			name:           "explicit true overrides false",
			handlerEnabled: "enabled: true",
			want:           true,
		},
		{
			name:           "omitted handler enabled is false",
			defaultEnabled: true,
		},
		{
			name:           "both enabled",
			defaultEnabled: true,
			handlerEnabled: "enabled: true",
			want:           true,
		},
	}
	for _, tt := range tests {
		for _, separate := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/separate=%v", tt.name, separate), func(t *testing.T) {
				data := fmt.Sprintf(`__default__:
  enabled: %t
  separate_hr_per_handler_enabled: %t
  window_size: 42
  budget: 0.2
handlers:
  - method: /test.Service/Configured
    %s
`, tt.defaultEnabled, separate, tt.handlerEnabled)
				var cfg Config
				if err := yaml.Unmarshal([]byte(data), &cfg); err != nil {
					t.Fatal(err)
				}
				h := NewHedger(cfg)

				// Сначала создаём default state, чтобы проверить и общий режим стратегии.
				if _, enabled := h.getByMethod("/test.Service/Other"); enabled != tt.defaultEnabled {
					t.Fatalf("unlisted method enabled = %v, want %v", enabled, tt.defaultEnabled)
				}
				for range 2 {
					state, enabled := h.getByMethod("/test.Service/Configured")
					if enabled != tt.want || (state != nil) != tt.want {
						t.Fatalf("configured method enabled = %v, state = %v, want %v", enabled, state, tt.want)
					}
				}
				handler := h.state.Load().handlersConfig["/test.Service/Configured"]
				if handler.WindowSize != 42 || handler.HedgeBudget != 0.2 {
					t.Fatalf("numeric defaults were not inherited: %+v", handler.MainConfig)
				}
			})
		}
	}
}
