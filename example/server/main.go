package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gfx-labs/sse"
)

func main() {
	http.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		stream, err := sse.Upgrade(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-t.C:
				if err := stream.Send(&sse.Event{Event: "ping", Data: []byte("foo")}); err != nil {
					return
				}
			}
		}
	})
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
