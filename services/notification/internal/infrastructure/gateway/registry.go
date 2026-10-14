package gateway

import (
	"log/slog"

	"notification/internal/infrastructure/gateway/push"
)

type Registry struct {
	Push *push.Client
}

func NewRegistry() *Registry {
	return &Registry{
		Push: push.NewClient(slog.Default()),
	}
}
