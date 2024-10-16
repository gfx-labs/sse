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
