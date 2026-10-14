package usecase

import (
	"notification/internal/application/usecase/notification/activated"
	"notification/internal/application/usecase/notification/canceled"
	"notification/internal/application/usecase/notification/confirmed"
	"notification/internal/infrastructure/gateway"
)

type Registry struct {
	NotifyConfirmed *confirmed.UseCase
	NotifyActivated *activated.UseCase
	NotifyCanceled  *canceled.UseCase
}

func NewRegistry(gateway *gateway.Registry) *Registry {
	return &Registry{
		NotifyConfirmed: confirmed.New(gateway.Push),
		NotifyActivated: activated.New(gateway.Push),
		NotifyCanceled:  canceled.New(gateway.Push),
	}
}
