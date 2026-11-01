package retry

func overrideIfZero[T comparable](val T, def T) T {
	var zero T
	if val == zero {
		return def
	}

	return val
}

func overrideIfNilSlice[T any](val []T, def []T) []T {
	if val == nil {
		return def
	}

	return val
}
