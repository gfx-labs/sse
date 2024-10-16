# sse

this is an implementation of https://html.spec.whatwg.org/multipage/server-sent-events.html


## eventsource

the `eventsource` package provides a basic implementation of the `EventSource` server.

as there are multiple ways to implement `Last-Event-ID`, it should be used as a reference in order to implement your own EventSource server.


## examples

### sse client

```go
package main

import (
    "context"
    "log"
    "net/http"

    "github.com/gfx-labs/sse"
)

func main() {
    req, err := http.NewRequest("GET", "http://localhost:8080/sse", nil)
    if err != nil {
        panic(err)
    }

    err = sse.Subscribe(context.Background(), req, func(msg *sse.Event) {
        log.Printf("msg: %s", string(msg.Data))
    })
    if err != nil {
        panic(err)
    }
    done := make(chan os.Signal, 1)
    signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
    <-done
}
```

### server

```go
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gfx-labs/sse"
	"github.com/gfx-labs/sse/eventsource"
)

func main() {
	srv := eventsource.NewServer(sse.DefaultUpgrader)
	go func() {
		for {
			time.Sleep(1 * time.Second)
			err := srv.Encode(&sse.Event{
				Event: []byte("ping"),
				Data:  []byte("foo"),
			})
			if err != nil {
				log.Println(err)
			}
		}
	}()
	http.Handle("/sse", srv)
	log.Println("listening on :8080")
	http.ListenAndServe(":8080", nil)
}
```

### eventsource

a basic eventsource implementation is provided in eventsource/eventsource.go

as there are multiple ways to implement `Last-Event-ID`, it should be used as a reference in order to implement your own EventSource server.

for more customized sse logic, you should use the `sse.Upgrader` and write to the EventSink directly.



