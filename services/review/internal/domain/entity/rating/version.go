package rating

import "github.com/samber/lo"

type version struct {
	Old int64
	new *int64
}

func (v *version) inc() {
	v.new = lo.ToPtr(v.Old + 1)
}

func (v version) current() int64 {
	if v.new != nil {
		return *v.new
	}
	return v.Old
}

func (v *Rating) Version() int64 {
	return v.version.current()
}

func (v *Rating) OldVersion() int64 {
	return v.version.Old
}
