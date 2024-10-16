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
