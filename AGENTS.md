# AGENTS.md

## Project Overview

Go library implementing the Server-Sent Events (SSE) specification. Module path: `github.com/gfx-labs/sse`. Minimum Go version: 1.27.

Provides: Reader/Writer (tokenizer + wire format), Encoder/Decoder (Event-level), Upgrader/EventSink (server-side HTTP), Client (Subscribe/SubscribeWithRetry), and an `eventsource` sub-package (reference EventSource server with pub/sub and B-tree event log).

## Build / Test / Lint Commands

```bash
# Run all tests
go test -v ./...

# Run tests for root package only
go test -v .

# Run tests for eventsource sub-package
go test -v ./eventsource/...

# Run a single test by name
go test -v -run TestClientBasic .
go test -v -run TestEncoderSimple .

# Lint (requires golangci-lint installed)
golangci-lint run ./...

# Build (library only, no binary)
go build ./...

# Build examples
go build ./example/...
```

There is no Makefile. CI runs `go vet`, `go test -race ./...` and golangci-lint via GitHub Actions on push/PR to master.

## Code Style Guidelines

### Package & Import Organization

- Two import groups separated by a blank line: stdlib first, then everything else (third-party and own sub-packages together).
- Alphabetical order within each group.
- No separate group for project-internal imports.

```go
import (
    "context"
    "fmt"
    "io"

    "github.com/cenkalti/backoff/v4"
    "github.com/gfx-labs/sse"
)
```

### Naming Conventions

- **Exported types**: PascalCase — `Event`, `Encoder`, `Decoder`, `Reader`, `Writer`, `Upgrader`, `EventSink`.
- **Unexported types**: camelCase — `readerState`, `writeState`, `eventLog`.
- **Constants**: iota enums use a shared prefix — `TokenInvalid`, `TokenSkip`, `TokenBom`, `TokenComment`, `TokenField`, `TokenDispatch`.
- **Sentinel errors**: `Err` prefix, defined in `errors.go` — `ErrInvalidUTF8Bytes`, `ErrInvalidContentType`, etc.
- **Constructors**: `NewX` for exported (`NewReader`, `NewEncoder`, `NewServer`), `newX` for unexported (`newEventLog`).
- **Option constructors**: `Option` prefix — `OptionValidateUtf8`, `OptionMaxBufferSize`; `WithHTTPClient` sets the client for `Subscribe`.
- **Receiver names**: Single letter, typically the first letter of the type (`r` for Reader, `d` for Decoder, `s` for Server). Note: `e` is reused across Writer, Encoder, EventSink, and Options.

### Types & Structs

- Exported fields are user-facing data; unexported fields hold internal state.
- Use `*[]byte` (pointer to slice) for optional fields where nil vs empty matters (see `Event.ID`).
- Factor internal state into separate unexported structs (`readerState`, `writeState`), embedded as value fields.
- Prefer `[]byte` over `string` throughout to avoid allocation from conversions when working with `io.Reader`/`io.Writer`.
- `Decoder.Decode` overwrites the passed `Event` completely, so reused events never carry stale fields.
- No custom interfaces are defined. Rely on stdlib interfaces (`io.Reader`, `io.Writer`, `http.Flusher`, `http.Handler`).
- No generics (aside from third-party `btree.BTreeG`).

### Functional Options Pattern

Options are passed via variadic `...Option` parameters:

```go
type Option func(*Options)

func NewReader(r io.Reader, opts ...Option) *Reader
```

### Error Handling

- Define sentinel errors with `errors.New` in a `var` block in `errors.go`.
- Wrap errors with `fmt.Errorf("%w: ...", ErrSentinel, ...)` for context.
- Check errors with `errors.Is`.
- No custom error types (struct-based); only sentinel values.
- Check errors immediately after every call; return early on failure.
- Use `panic` only for programmer misuse (nil arguments to methods that cannot accept them).

```go
if err := e.wr.Field("event", p.Event); err != nil {
    return err
}
```

### Context Usage

- `context.Context` is always the first parameter.
- Thread context via `r.Clone(ctx)` so the caller's request is not mutated.
- Check cancellation with `select` on `ctx.Done()` using a `default` case for non-blocking checks.

### Concurrency

- Document thread safety on types: "NOT thread safe" or similar.
- Use `sync.Mutex`/`sync.RWMutex` for shared state (subscribers, the event log), `sync/atomic` for counters.
- Wake subscribers with a capacity-1 channel and non-blocking send so broadcasts never block or get lost.

### Comments

- Comments are sparse and pragmatic. Not every function needs a comment.
- Document thread safety on types that are not goroutine-safe.
- Keep comments short and factual. No embellishment.

### Testing

- Use `github.com/stretchr/testify` — `require` for fatal checks, `assert` for non-fatal.
- Use `require.Equalf(t, ..., "context: %d", idx)` for diagnostic messages in loops.
- Table-driven tests with inline `testCase` struct and `for range` loop (no `t.Run` subtests).
- Test naming: `TestXxxYyy` — `TestClientBasic`, `TestEncoderSimple`, `TestReaderSimple`.
- Integration tests use `httptest.NewServer` with inline `http.HandlerFunc`.
- Mixed test package naming: `package sse_test` (black-box) and `package sse` (white-box) both appear.

```go
type testCase struct {
    xs []*Event
    w  string
}
cases := []testCase{{...}, {...}}
for idx, v := range cases {
    // ...
    require.Equalf(t, expected, actual, "idx: %d", idx)
}
```

### Architecture Patterns

- **Lexer/parser two-stage pipeline**: `Reader` (tokenizer) -> `Decoder` (event builder). Inverse: `Encoder` -> `Writer`.
- **Pull-based iterator**: `Reader.Next()` / `Reader.Token()` — caller pulls tokens, similar to `encoding/xml`.
- **Upgrader pattern**: Struct with `Upgrade` method plus `DefaultUpgrader` package-level instance (similar to gorilla/websocket).
- **Defensive copying**: Use `copyBytes` helper to copy byte slices before storing, preventing aliasing with internal buffers.
- **Manual byte-level parsing**: No `bufio.Scanner` or regex. State machine handles `\n`, `\r`, `\r\n` line endings per the SSE spec.

### Linter Configuration

`.golangci.yml` uses golangci-lint v2 with the standard linters plus `bodyclose`, `errorlint`, `misspell`, `unconvert`, and the `gofmt` formatter.

### File Organization

| File | Purpose |
|------|---------|
| `reader.go` | Lexer/tokenizer — produces `Token` values from byte stream |
| `writer.go` | Low-level SSE wire format writer |
| `decoder.go` | Interprets tokens into `Event` structs |
| `encoder.go` | Encodes `Event` structs to SSE wire format |
| `event.go` | `Event` struct definition |
| `errors.go` | Sentinel error values |
| `options.go` | Functional options (`Option`, `Options`) |
| `client.go` | `Subscribe`, `SubscribeWithRetry`, `AddSSEHeaders` |
| `upgrade.go` | `Upgrader`, `EventSink` for server-side SSE |
| `eventsource/server.go` | EventSource server with pub/sub |
| `eventsource/event_log.go` | B-tree backed event log |
