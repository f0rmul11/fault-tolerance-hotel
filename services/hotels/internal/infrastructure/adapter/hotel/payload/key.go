package payload

import "github.com/gofrs/uuid"

// HotelKey включает версию формата payload, а не версию данных отеля.
func HotelKey(id uuid.UUID) string {
	return "v2:hotel:" + id.String()
}
