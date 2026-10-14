package list

import "github.com/samber/mo"

type Input struct {
	Limit  mo.Option[int]
	Offset int
}
