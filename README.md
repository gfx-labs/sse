# sse

Go implementation of [server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html): a client that reconnects as the spec describes, a server-side stream, and an encoder and decoder for the wire format. Requires Go 1.27.

```
go get github.com/gfx-labs/sse
```

## Client

`Client.Events` returns an iterator. It reconnects after disconnects, sends `Last-Event-ID`, and honors the server's `retry:` delay. It stops when the context ends, on a `204 No Content`, or on a response that should not be retried.

```go
var c sse.Client
req, _ := http.NewRequest("GET", "http://localhost:8080/sse", nil)
for ev, err := range c.Events(ctx, req) {
	if err != nil {
		return err
	}
	log.Printf("%s: %s", ev.Event, ev.Data)
}
```

The yielded `*sse.Event` is reused for the next event. Call `ev.Clone()` to keep it.

## Server

```go
http.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
	stream, err := sse.Upgrade(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for msg := range updates(r.Context()) {
		if err := stream.Send(&sse.Event{Event: "update", Data: msg}); err != nil {
			return
		}
	}
})
```

`stream.LastEventID` holds the id a reconnecting client sent. `stream.Comment("")` writes a keep-alive.

## Encoding and decoding

`NewEncoder` and `NewDecoder` work on any `io.Writer` and `io.Reader`. `sse.Events(r)` iterates over the events in a reader. Neither allocates per event in steady state.

## eventsource

`eventsource.NewServer(logSize)` is a reference `http.Handler` that publishes events to every client, keeps the newest `logSize` events, and replays missed events to clients that reconnect with `Last-Event-ID`. See `example/eventsource`.
