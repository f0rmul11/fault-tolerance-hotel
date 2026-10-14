package checkin

import (
	"github.com/gofrs/uuid"

	"check-in/internal/domain/entity"
)

type Context struct {
	ActorID   uuid.UUID
	RequestID uuid.UUID
	Role      entity.Role
}
