package sse

import "net/http"

type Options struct {
	validateUTF8  bool
	maxBufferSize int
	httpClient    *http.Client
}

func newOptions(opts []Option) *Options {
	o := &Options{}
	for _, v := range opts {
		v(o)
	}
	return o
}

func (e *Options) ValidateUTF8() bool {
	return e.validateUTF8
}

// MaxBufferSize is the longest line, in bytes, the Reader accepts. Zero means unlimited.
func (e *Options) MaxBufferSize() int {
	return e.maxBufferSize
}

func (e *Options) client() *http.Client {
	if e.httpClient != nil {
		return e.httpClient
	}
	return http.DefaultClient
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

// WithHTTPClient sets the client used by Subscribe and SubscribeWithRetry.
func WithHTTPClient(c *http.Client) Option {
	return func(o *Options) {
		o.httpClient = c
	}
}
