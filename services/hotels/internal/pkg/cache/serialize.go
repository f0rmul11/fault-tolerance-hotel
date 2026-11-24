package cache

// Marshaller marshall
type Marshaller interface {
	MarshalJSON() ([]byte, error)
}

// UnMarshaller unmarshall
type UnMarshaller interface {
	UnmarshalJSON(data []byte) error
}

func marshall[TValue Marshaller](v any) ([]byte, error) {
	return v.(TValue).MarshalJSON()
}

func unmarshal[TValuePtr UnMarshaller](data []byte, v any) error {
	return (v).(TValuePtr).UnmarshalJSON(data)
}
