package llmusage

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidOptions  = errors.New("llmusage: invalid options")
	ErrUnsupported     = errors.New("llmusage: unsupported protocol or format")
	ErrMalformedStream = errors.New("llmusage: malformed stream")
	ErrLimitExceeded   = errors.New("llmusage: limit exceeded")
	ErrFinished        = errors.New("llmusage: decoder finished")
)

// ParseError adds protocol, format, parser stage, and byte offset to a decoder
// error. Use errors.Is and errors.As to inspect it.
type ParseError struct {
	Protocol Protocol
	Format   Format
	Stage    string
	Offset   int64
	Err      error
}

func (e *ParseError) Error() string {
	if e == nil {
		return "<nil>"
	}
	location := ""
	if e.Offset > 0 {
		location = fmt.Sprintf(" at byte %d", e.Offset)
	}
	return fmt.Sprintf("llmusage: parse %s/%s during %s%s: %v", e.Protocol, e.Format, e.Stage, location, e.Err)
}

func (e *ParseError) Unwrap() error { return e.Err }
