package hotel

type Rating struct {
	// Degraded means the value and count are unknown, not zero.
	Degraded    bool
	Value       float64
	ReviewCount int64

	// Version — версия агрегата в Review, не CAS-токен кеша.
	Version int64
}
