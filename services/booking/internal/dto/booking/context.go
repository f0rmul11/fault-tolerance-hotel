package booking

import (
	"github.com/gofrs/uuid"

	"booking/internal/domain/entity"
)

// Context содержит проверенные транспортом данные вызывающего.
type Context struct {
	RequestID uuid.UUID
	ActorID   uuid.UUID
	Role      entity.Role
}
