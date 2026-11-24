package add

import "github.com/gofrs/uuid"

type Input struct {
	HotelID uuid.UUID
	Stars   int64
}
