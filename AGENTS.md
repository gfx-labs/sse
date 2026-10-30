# AGENTS.md

## Project Overview

Go library implementing the Server-Sent Events (SSE) specification. Module path: `github.com/gfx-labs/sse`. Minimum Go version: 1.27. There are no external consumers yet, so breaking API changes are acceptable.

Provides: `Encoder`/`Decoder` and the `Events` iterator (wire format), `Upgrade`/`Stream` (server side), `Client` (reconnecting consumer), and the `eventsource` sub-package (reference server with an in-memory event log and Last-Event-ID replay).

## Commands

```bash
go test -race ./...                     # all tests
go test -run TestSpec ./e2e             # spec and WPT conformance tests
go test -run '^$' -bench . -count 6 .   # benchmarks; compare with benchstat
golangci-lint run ./...                 # v2 config in .golangci.yml
go build ./example/...
```

CI runs `go vet`, `go test -race ./...` and golangci-lint on push and PR to master.

## Design

- `Event` uses `string` for the event name and `[]byte` for `ID` and `Data`. nil vs empty is meaningful: nil `Data` means no data field (browsers do not dispatch it), and an empty non-nil `ID` resets the client's last event id. `Retry` is a `time.Duration`; `Fields` holds only unknown fields.
- `Decoder.Decode` reuses the passed `Event`'s buffers. Data is valid until the next call; `Clone` copies it. Comment-only blocks and blank lines do not produce events.
- `lineReader` (`linereader.go`) scans whole lines from its own buffer and handles `\r\n`, `\r`, `\n`, a leading BOM, and `WithMaxLineSize`.
- `Encoder` builds each event in a reused buffer and sends it in one `Write`, so `Stream.Send` flushes whole events. Field names and values containing line breaks are rejected with `ErrInvalidField`.
- `Upgrade` flushes through `http.NewResponseController`, so wrapped `ResponseWriter`s that implement `Unwrap` work.
- `Client.Events` returns `iter.Seq2[*Event, error]`. It reconnects with Last-Event-ID and the server's retry delay, doubles the delay on consecutive failures up to `MaxRetry`, stops on 204, and yields a final error for non-retryable statuses or a wrong Content-Type.
- Hot paths should not allocate per event. Check `-benchmem` before and after changes to `decoder.go`, `encoder.go` or `linereader.go`.

## Conventions

- Imports: stdlib group, then everything else.
- Options are `WithX` functions of type `Option`.
- Sentinel errors live in `errors.go`, prefixed `sse:`; wrap with `fmt.Errorf("%w: ...")` and check with `errors.Is`.
- `context.Context` is the first parameter. Clone requests with `r.Clone(ctx)`.
- Concurrency: document thread safety on types. Use `sync.Mutex`/`sync.RWMutex` for shared state and capacity-1 channels with non-blocking sends to wake subscribers.
- Comments are short and factual and explain non-obvious constraints.

## Testing

- testify: `require` for fatal checks, `assert` otherwise.
- Table-driven tests may use `t.Run` subtests.
- Black-box tests (`package sse_test`, `e2e`) are preferred; `e2e` holds spec and Web Platform Test vectors.
- HTTP tests use `httptest.NewServer`.

## Files

| File | Purpose |
|------|---------|
| `event.go` | `Event`, `Clone`, buffer reuse |
| `linereader.go` | Line scanner used by the decoder |
| `decoder.go` | `Decoder`, `Events` iterator, field parsing |
| `encoder.go` | `Encoder`, wire format output |
| `server.go` | `Upgrade`, `Stream` |
| `client.go` | `Client`, reconnect and backoff |
| `options.go` | `Option`, `WithMaxLineSize`, `WithUTF8Validation` |
| `errors.go` | Sentinel errors |
| `eventsource/server.go` | Reference server with heartbeats and replay |
| `eventsource/event_log.go` | B-tree event log with optional size limit |
