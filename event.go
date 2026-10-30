package sse

// Event holds all of the event source fields
type Event struct {
	Event []byte
	ID    *[]byte
	Data  []byte

	Fields   map[string][]byte
	Comments [][]byte
}

func (e *Event) hasContent() bool {
	return e.ID != nil || e.Data != nil || len(e.Event) > 0 || len(e.Fields) > 0
}
