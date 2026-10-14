package storage

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"review/internal/infrastructure/storage/rating"
	"review/internal/pkg/transaction/wrapper"
)

type Registry struct {
	Ratings *rating.Storage
}

func NewRegistry(pool *pgxpool.Pool) *Registry {
	return &Registry{Ratings: rating.New(wrapper.NewDatabase(pool))}
}
