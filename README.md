# sse

Go implementation of [server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html): a client that reconnects as the spec describes, a server-side stream, and an encoder and decoder for the wire format. Requires Go 1.27.

```
go get github.com/gfx-labs/sse
```

## Client

`eventsource.Client.Events` returns an iterator. It reconnects after disconnects, sends `Last-Event-ID`, and passes the server's `retry:` delay to its reconnect policy. It stops when the context ends, on a `204 No Content`, or when the policy declines. A `Last-Event-ID` header on the request is the starting point.

```go
var c eventsource.Client
req, _ := http.NewRequest("GET", "http://localhost:8080/sse", nil)
for ev, err := range c.Events(ctx, req) {
	if err != nil {
		return err
	}
	log.Printf("%s: %s", ev.Event, ev.Data)
}
```

The yielded `*sse.Event` is reused for the next event. Call `ev.Clone()` to keep it.

`Client.Reconnect` is a `func(eventsource.Attempt) (time.Duration, bool)` that sees the error, consecutive failure count, server retry delay and last event id, and returns the delay or false to stop. `Attempt.Failures` counts connections that ended before the server answered with an event stream, so it is 1 after any stream that was established. The default is `eventsource.Backoff(3*time.Second, 30*time.Second)`: exponential backoff with jitter that stops on errors `eventsource.Retryable` reports as permanent, including requests `net/http` refuses to send. A server `retry:` delay above the maximum is used as is. `eventsource.NoReconnect` stops after one connection.

```go
backoff := eventsource.Backoff(time.Second, time.Minute)
c := eventsource.Client{Reconnect: func(a eventsource.Attempt) (time.Duration, bool) {
	if se, ok := errors.AsType[*sse.StatusError](a.Err); ok && se.Code == http.StatusUnauthorized {
		refreshToken()
		return 0, true
	}
	if a.Failures > 10 {
		return 0, false
	}
	return backoff(a)
}}
```

For one connection without reconnects, `sse.Connect(httpClient, req)` sends the request with event-stream headers and checks the status and Content-Type, returning `sse.ErrNoContent` or `*sse.StatusError`. Read the body with `sse.Events(resp.Body)`.

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

`Upgrade` clears the write deadline set by `http.Server.WriteTimeout`, which would otherwise end the stream. Use `stream.SetWriteDeadline` to bound a write to a slow client. `sse.ErrStreamingNotSupported` means the `ResponseWriter` cannot flush and nothing was written, so the handler can still send an error status.

## Encoding and decoding

`NewEncoder` and `NewDecoder` work on any `io.Writer` and `io.Reader`. `sse.Events(r)` iterates over the events in a reader. Neither allocates per event in steady state.

## eventsource

`eventsource.NewServer(logSize)` is a reference `http.Handler` that publishes events to every client, keeps the newest `logSize` events (`DefaultLogSize`, 1024, when `logSize <= 0`) in a ring buffer, and replays missed events to clients that reconnect with `Last-Event-ID`. A client without an id from this server gets the latest event and then everything published after it connected. See `example/eventsource`.

`Publish` returns an error, such as `sse.ErrInvalidField`, for an event that cannot be encoded, and does not store it. `Server.WriteTimeout` (30 seconds by default) ends a stream whose client stops reading.

`http.Server.Shutdown` waits for open streams, so close the event source when shutdown starts:

```go
es := eventsource.NewServer(0)
httpServer.RegisterOnShutdown(es.Close)
```
