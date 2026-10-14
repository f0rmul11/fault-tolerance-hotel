package dao

import (
	"check-in/internal/domain/entity"
	checkindto "check-in/internal/dto/checkin"
	"check-in/internal/pkg/xo"
)

type Request struct{ xo.Request }

func (r Request) ConvertTo() checkindto.Request {
	return checkindto.Request{
		ActorID:   r.ActorID,
		Key:       r.Key,
		BookingID: r.BookingID.UUID,
		Action:    entity.Action(r.Action.String),
	}
}
