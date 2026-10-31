package sse

import "errors"

var (
	ErrInvalidUTF8           = errors.New("sse: invalid utf8")
	ErrInvalidContentType    = errors.New("sse: invalid content type")
	ErrStreamingNotSupported = errors.New("sse: streaming not supported")
	ErrLineTooLong           = errors.New("sse: line exceeds max line size")
	ErrInvalidField          = errors.New("sse: field name or value contains a line break or invalid character")
	// ErrNoContent means the server responded 204 No Content, which tells clients to stop reconnecting.
	ErrNoContent = errors.New("sse: server responded 204 No Content")
)
