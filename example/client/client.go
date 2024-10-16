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
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	<-done
}
