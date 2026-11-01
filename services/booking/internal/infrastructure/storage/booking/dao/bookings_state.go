package dao

import (
	"booking/internal/domain/vo"
	"booking/internal/pkg/xo"
)

type BookingsState struct {
	xo.BookingsState
}

func (row BookingsState) ConvertTo() vo.Version {
	return vo.Version{Old: row.Version.Int64}
}
