package ldservice

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/tidwall/btree"
)

type CreateStreamParams struct {
	Tag            string            `json:"tag"`
	CallbackURL    string            `json:"callbackUrl"`
	StreamURL      string            `json:"streamUrl"`
	InitialDelayMS *int              `json:"initialDelayMs,omitempty"`
	LastEventID    string            `json:"lastEventId,omitempty"`
	Method         string            `json:"method,omitempty"`
	Body           string            `json:"body,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	ReadTimeoutMS  *int              `json:"readTimeoutMs,omitempty"`
}

type CommandParams struct {
	Command string        `json:"command"`
	Listen  *ListenParams `json:"listen"`
}

type ListenParams struct {
	Type string `json:"type"`
}

type Stream struct {
	Params CreateStreamParams
}

type ContractServer struct {
	streams btree.Map[string, *Stream]
}

func (c *ContractServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.WriteHeader(200)
		return
	case http.MethodDelete:
		if urlPath := r.URL.Path; urlPath != "" {
			_, ok := c.streams.Delete(urlPath)
			if ok {
				w.WriteHeader(200)
				return
			}
			w.WriteHeader(404)
		} else {
			os.Exit(0)
		}
	case http.MethodPost:
		c.handlePost(w, r)
	}
}

func (c *ContractServer) handlePost(w http.ResponseWriter, r *http.Request) {
	if urlPath := r.URL.Path; urlPath != "" {

	} else {
		s := &Stream{}
		err := json.NewDecoder(r.Body).Decode(&s.Params)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		c.streams.Set(s.Params.StreamURL)
	}
}
