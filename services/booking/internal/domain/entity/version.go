package entity

import (
	"github.com/samber/lo"
)

type version struct {
	Old int
	new *int
}

func (v *version) inc() {
	v.new = lo.ToPtr(v.Old + 1)
}

func (v version) current() int {
	if v.new != nil {
		return *v.new
	}
	return v.Old
}
