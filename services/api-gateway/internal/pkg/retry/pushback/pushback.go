package pushback

import (
	"strconv"
	"time"

	"google.golang.org/grpc/metadata"
)

type Pushback struct {
	Delay  time.Duration
	Reject bool
	Has    bool
}

func Parse(md metadata.MD) Pushback {
	if md == nil {
		return Pushback{}
	}

	pairs := md.Get("grpc-retry-pushback-ms")
	if len(pairs) == 0 {
		return Pushback{}
	}

	if len(pairs) != 1 {
		return Pushback{
			Has:    true,
			Reject: true,
		}
	}
	n, err := strconv.ParseInt(pairs[0], 10, 32)
	if err != nil {
		// Некорректный или переполненный pushback запрещает повтор.
		return Pushback{
			Has:    true,
			Reject: true,
		}
	}

	pb := Pushback{Has: true}

	if n < 0 {
		pb.Reject = true
		return pb
	}

	pb.Delay = time.Duration(n) * time.Millisecond
	return pb
}
