package sse

import (
	"bufio"
	"net/http"
	"strings"
)

// EventSink tracks a event source connection between a client and a server
// note that they are NOT thread safe
type EventSink struct {
	wr  http.ResponseWriter
	r   *http.Request
	bw  *bufio.Writer
	enc *Encoder

	LastEventId string
}

// The idea is that the user may parse the request body beforehand
// and then pass wr and r into this function, upgrading to an sse stream
// this is similar to how the websocket libraries in golang work
func Upgrade(wr http.ResponseWriter, r *http.Request) (*EventSink, error) {
	flusher, ok := wr.(http.Flusher)
	if !ok {
		return nil, ErrStreamingNotSupported
	}

	if !strings.EqualFold(r.Header.Get("Content-Type"), "text/event-stream") {
		return nil, ErrInvalidContentType
	}
	o := &EventSink{
		wr: wr,
		r:  r,
		bw: bufio.NewWriter(wr),
	}
	o.LastEventId = r.Header.Get("Last-Event-ID")

	wr.Header().Add("Content-Type", "text/event-stream")
	wr.Header().Set("Cache-Control", "no-cache")
	wr.Header().Set("Connection", "keep-alive")
	flusher.Flush()
	o.enc = NewEncoder(o.bw)
	return o, nil
}

func (e *EventSink) Encode(p *Event) error {
	err := e.enc.Encode(p)
	if err != nil {
		return err
	}
	return e.bw.Flush()
}
