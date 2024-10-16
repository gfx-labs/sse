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
}
