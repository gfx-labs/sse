package sse_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gfx-labs/sse"
	"github.com/stretchr/testify/require"
)

var urlPath string
var server *httptest.Server

var mldata = `{
	"key": "value",
	"array": [
		1,
		2,
		3
	]
}`

func TestClientBasic(t *testing.T) {
	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sink, err := sse.DefaultUpgrader.Upgrade(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for i := 0; i < 5; i++ {
			payload := strconv.Itoa(i)
			err = sink.Encode(&sse.Event{
				Data: bytes.NewBuffer([]byte(payload)),
			})
		}
	})
	srv := httptest.NewServer(httpHandler)
	defer srv.Close()

	ctx, cn := context.WithCancel(context.Background())
	defer cn()
	req, err := http.NewRequest("GET", srv.URL, nil)
	require.NoError(t, err)

	idx := 0
	err = sse.Subscribe(ctx, req, func(msg *sse.Event) {
		bts, _ := io.ReadAll(msg.Data)
		val, _ := strconv.Atoi(string(bts))
		require.Equal(t, idx, val)
		idx++
	})
	require.NoError(t, err)
	require.Equal(t, 5, idx)
}
