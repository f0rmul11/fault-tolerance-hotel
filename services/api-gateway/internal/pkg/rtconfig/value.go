package rtconfig

import "time"

// Type задаёт тип значения, проверяемый при загрузке конфигурации.
type Type string

const (
	BoolType     Type = "bool"
	StringType   Type = "string"
	IntType      Type = "int"
	Int64Type    Type = "int64"
	UintType     Type = "uint"
	Uint64Type   Type = "uint64"
	Float64Type  Type = "float64"
	DurationType Type = "duration"
)

// Value — неизменяемый скаляр. Getter другого типа возвращает ноль и не меняет Err.
// String возвращает заранее подготовленное представление любого поддержанного типа.
type Value interface {
	Bool() bool
	String() string
	Int() int
	Int64() int64
	Uint() uint
	Uint64() uint64
	Float64() float64
	Duration() time.Duration
	Err() error
	Type() Type
}

type scalarValue struct {
	typ      Type
	text     string
	boolean  bool
	signed   int64
	unsigned uint64
	floating float64
	duration time.Duration
	err      error
}

// NewNilValue сохраняет причину отсутствия значения и предоставляет безопасные getters.
func NewNilValue(err error) Value {
	return &scalarValue{
		err: err,
	}
}

func (v *scalarValue) Bool() bool {
	return v.typ == BoolType && v.boolean
}

func (v *scalarValue) String() string {
	return v.text
}

func (v *scalarValue) Int() int {
	if v.typ != IntType && v.typ != Int64Type {
		return 0
	}

	result := int(v.signed)
	if int64(result) != v.signed {
		return 0
	}
	return result
}

func (v *scalarValue) Int64() int64 {
	if v.typ != IntType && v.typ != Int64Type {
		return 0
	}
	return v.signed
}

func (v *scalarValue) Uint() uint {
	if v.typ != UintType && v.typ != Uint64Type {
		return 0
	}

	result := uint(v.unsigned)
	if uint64(result) != v.unsigned {
		return 0
	}
	return result
}

func (v *scalarValue) Uint64() uint64 {
	if v.typ != UintType && v.typ != Uint64Type {
		return 0
	}
	return v.unsigned
}

func (v *scalarValue) Float64() float64 {
	if v.typ != Float64Type {
		return 0
	}
	return v.floating
}

func (v *scalarValue) Duration() time.Duration {
	if v.typ != DurationType {
		return 0
	}
	return v.duration
}

func (v *scalarValue) Err() error {
	return v.err
}

func (v *scalarValue) Type() Type {
	return v.typ
}
