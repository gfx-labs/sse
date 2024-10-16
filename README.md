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

	"os"
	"os/signal"
	"syscall"

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

### sse server

```go
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gfx-labs/sse"
)

func main() {
	http.Handle("/sse", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := sse.DefaultUpgrader.Upgrade(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for {
			err = conn.Encode(&sse.Event{Event: []byte("ping"), Data: []byte("foo")})
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			time.Sleep(1 * time.Second)
		}
	}))
	log.Println("listening on :8080")
	http.ListenAndServe(":8080", nil)
}
```

### eventsource server

a basic eventsource implementation is provided in eventsource/eventsource.go

see an example of a server+client in examples/eventsource/main.go

as there are multiple ways to implement `Last-Event-ID`, it should be used as a reference in order to implement your own EventSource server.



