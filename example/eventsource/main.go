package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gfx-labs/sse"
	"github.com/gfx-labs/sse/eventsource"
)

func main() {
	srv := eventsource.NewServer(1000)
	go func() {
		for range time.Tick(time.Second) {
			srv.Publish(&sse.Event{Event: "ping", Data: []byte("foo")})
		}
	}()
	go func() {
		time.Sleep(time.Second)
		req, err := http.NewRequest("GET", "http://localhost:8080/sse", nil)
		if err != nil {
			log.Fatal(err)
		}
		var c sse.Client
		for ev, err := range c.Events(context.Background(), req) {
			if err != nil {
				log.Fatal(err)
			}
			log.Println("got message", ev.Event, string(ev.Data), string(ev.ID))
		}
	}()
	http.Handle("/sse", srv)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
