package get

import "github.com/gofrs/uuid"

// Input задаёт отель и минимальную версию его рейтинга, известную клиенту.
type Input struct {
	HotelID          uuid.UUID
	MinRatingVersion int64
}
