package sse

import (
	"bufio"
	"net/http"
)

// EventSink tracks a event source connection between a client and a server
// note that they are NOT thread safe
type EventSink struct {
	wr      http.ResponseWriter
	r       *http.Request
	bw      *bufio.Writer
	enc     *Encoder
	flusher http.Flusher

	LastEventId string
}

type Upgrader struct {
}

var DefaultUpgrader = &Upgrader{}

// The idea is that the user may parse the request body beforehand
// and then pass wr and r into this function, upgrading to an sse stream
// this is similar to how the websocket libraries in golang work
func (u *Upgrader) Upgrade(wr http.ResponseWriter, r *http.Request) (*EventSink, error) {
	flusher, ok := wr.(http.Flusher)
	if !ok {
		return nil, ErrStreamingNotSupported
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
	wr.WriteHeader(200)
	flusher.Flush()
	o.flusher = flusher
	o.enc = NewEncoder(wr)
	return o, nil
}

func (e *EventSink) Encode(p *Event) error {
	select {
	case <-e.r.Context().Done():
		return e.r.Context().Err()
	default:
	}
	err := e.enc.Encode(p)
	if err != nil {
		return err
	}
	e.bw.Flush()
	e.flusher.Flush()
	return nil
}
