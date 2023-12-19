package sse

type Options struct {
	validateUTF8 bool
	encodeBase64 bool
}

func (e *Options) ValidateUTF8() bool {
	return e.validateUTF8
}

func (e *Options) EncodeBase64() bool {
	return e.validateUTF8
}

type Option func(*Options)

func OptionValidateUtf8(enable bool) Option {
	return func(o *Options) {
		o.validateUTF8 = enable
	}
}
func OptionEncodeBase64(enable bool) Option {
	return func(o *Options) {
		o.encodeBase64 = enable
	}
}
