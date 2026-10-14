package available

import (
	"github.com/gofrs/uuid"

	"booking/internal/domain/vo"
)

type Input struct {
	HotelID uuid.UUID
	Period  vo.Period
}
