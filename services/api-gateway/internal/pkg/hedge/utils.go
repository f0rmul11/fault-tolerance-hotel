package hedge

func overrideIfZero[T comparable](val T, def T) T {
	var zero T
	if val == zero {
		return def
	}

	return val
}
