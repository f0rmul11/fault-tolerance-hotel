package storage

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"booking/internal/infrastructure/storage/booking"
	"booking/internal/pkg/transaction/wrapper"
)

type Registry struct{ Bookings *booking.Storage }

func NewRegistry(pool *pgxpool.Pool) *Registry {
	return &Registry{Bookings: booking.New(wrapper.NewDatabase(pool))}
}
