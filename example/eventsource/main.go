package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gfx-labs/sse"
	"github.com/gfx-labs/sse/eventsource"
)

func server() {
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

func client() error {
	ctx, cn := context.WithCancel(context.Background())
	defer cn()
	req, err := http.NewRequest("GET", "http://localhost:8080/sse", nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	err = sse.Subscribe(ctx, req, func(msg *sse.Event) {
		log.Println("got message", string(msg.Event), string(msg.Data))
	})
	return err
}

func main() {
	go func() {
		time.Sleep(2 * time.Second)
		err := client()
		if err != nil {
			panic(err)
		}
	}()
	server()
}
