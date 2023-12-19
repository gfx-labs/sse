/* This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/. */

package sse

import (
	"io"
	"time"
)

// Event holds all of the event source fields
type Event struct {
	timestamp time.Time
	Event     []byte
	ID        *[]byte
	Data      io.Reader

	Fields map[string][]byte
}

func (e *Event) hasContent() bool {
	return e.ID != nil || e.Data != nil || len(e.Event) > 0 || len(e.Fields) > 0
}
