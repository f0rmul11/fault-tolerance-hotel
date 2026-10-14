package storage

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"check-in/internal/infrastructure/storage/checkin"
	"check-in/internal/pkg/transaction/wrapper"
)

type Registry struct{ CheckIns *checkin.Storage }

func NewRegistry(pool *pgxpool.Pool) *Registry {
	return &Registry{CheckIns: checkin.New(wrapper.NewDatabase(pool))}
}
