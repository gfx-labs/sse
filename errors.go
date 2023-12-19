package sse

import "errors"

var (
	ErrInvalidUTF8Bytes      = errors.New("invalid utf8 bytes")
	ErrInvalidContentType    = errors.New("invalid content type")
	ErrStreamingNotSupported = errors.New("streaming not supported")
	ErrUnknownTokenType      = errors.New("unknown token type")
	ErrInvalidToken          = errors.New("invalid token")
)
