package entity

type Status string

const (
	Expected  Status = "EXPECTED"
	Arriving  Status = "ARRIVING"
	CheckedIn Status = "CHECKED_IN"
	Canceling Status = "CANCELING"
	Canceled  Status = "CANCELED"
)

type Action string

const (
	Arrive Action = "ARRIVE"
	Cancel Action = "CANCEL"
)

type Role string

const (
	RoleReception Role = "RECEPTION"
	RoleOperator  Role = "OPERATOR"
)
