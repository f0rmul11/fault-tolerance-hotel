package arrivals

import (
	"github.com/samber/mo"

	"check-in/internal/domain/vo"
)

type Input struct{ Period mo.Option[vo.Period] }
