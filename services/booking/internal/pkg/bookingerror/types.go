package bookingerror

// Коды HotelFlow дополняют исходные коды ordererror.
const (
	PermissionDenied Code = 6
	Unavailable      Code = 7
	DeadlineExceeded Code = 8
	Canceled         Code = 9
	DataIntegrity    Code = 13
)
