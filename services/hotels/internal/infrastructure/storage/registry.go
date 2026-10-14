package storage

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"hotels/internal/infrastructure/storage/hotel"
	"hotels/internal/infrastructure/storage/room"
	"hotels/internal/pkg/transaction/wrapper"
)

type Registry struct {
	Hotels *hotel.Storage
	Rooms  *room.Storage
}

func NewRegistry(pool *pgxpool.Pool) *Registry {
	db := wrapper.NewDatabase(pool)
	return &Registry{
		Hotels: hotel.New(db),
		Rooms:  room.New(db),
	}
}
