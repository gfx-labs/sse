package sse

type options struct {
	maxLineSize  int
	validateUTF8 bool
}

// Option configures a Decoder, Encoder or Stream.
type Option func(*options)

func newOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithMaxLineSize limits the length of a decoded line in bytes. Longer lines fail
// with ErrLineTooLong. Zero, the default, means no limit.
func WithMaxLineSize(n int) Option {
	return func(o *options) { o.maxLineSize = n }
}

// WithUTF8Validation makes the Encoder reject events that are not valid UTF-8.
func WithUTF8Validation() Option {
	return func(o *options) { o.validateUTF8 = true }
}
