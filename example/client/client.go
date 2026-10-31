package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gfx-labs/sse/eventsource"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	req, err := http.NewRequest("GET", "http://localhost:8080/sse", nil)
	if err != nil {
		log.Fatal(err)
	}
	var c eventsource.Client
	for ev, err := range c.Events(ctx, req) {
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("%s: %s", ev.Event, ev.Data)
	}
}
