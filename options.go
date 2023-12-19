package sse

type Options struct {
	validateUTF8  bool
	maxBufferSize int
}

func (e *Options) ValidateUTF8() bool {
	return e.validateUTF8
}
func (e *Options) MaxBufferSize() int {
	return e.maxBufferSize
}

type Option func(*Options)

func OptionValidateUtf8(enable bool) Option {
	return func(o *Options) {
		o.validateUTF8 = enable
	}
}

func OptionMaxBufferSize(i int) Option {
	return func(o *Options) {
		o.maxBufferSize = i
	}
}
