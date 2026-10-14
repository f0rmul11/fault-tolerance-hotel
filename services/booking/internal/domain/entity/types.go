package entity

// Status — состояние бронирования.
type Status string

const (
	Confirmed Status = "CONFIRMED"
	Active    Status = "ACTIVE"
	Canceled  Status = "CANCELED"
)

type Role string

const (
	RoleGuest     Role = "GUEST"
	RoleReception Role = "RECEPTION"
	RoleOperator  Role = "OPERATOR"
)
