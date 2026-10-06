package iostreams

import "io"

// IOStreams keeps commands independent of process-wide stdin/stdout.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}
