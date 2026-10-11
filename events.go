// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package hue

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

// Event is one event of the bridge's event stream.
type Event struct {
	// Type is add, update, delete or error.
	Type string `json:"type"`
	// ID is the event's ID.
	ID string `json:"id"`
	// Created is when the bridge made the event.
	Created time.Time `json:"creationtime"`
	// Data are the resources the event is about: whole for add, partial
	// (id, type, owner and what changed) for update, id and type for
	// delete.
	Data []Resource `json:"data"`
}

// ErrStopped is returned by ReadEvents when a value arrived on stop.
var ErrStopped = errors.New("resynchronising")

// ErrStreamClosed is returned by ReadEvents when the bridge ended the
// stream.
var ErrStreamClosed = errors.New("the bridge closed the stream")

// ReadEvents reads server-sent events from r and hands each message's
// events to f: the data lines of one message are joined (the SSE framing)
// and a message's data is a JSON array of events; comments and other
// fields are skipped. It returns ErrStreamClosed when r ends, the read
// error when reading fails, and ErrStopped when a value arrives on stop
// (checked after each line; nil never stops).
func ReadEvents(r io.Reader, f func([]Event), stop <-chan struct{}) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	var data []string
	flush := func() {
		if len(data) == 0 {
			return
		}
		var evs []Event
		if json.Unmarshal([]byte(strings.Join(data, "\n")), &evs) == nil && len(evs) > 0 {
			f(evs)
		}
		data = data[:0]
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, ":"): // comment ("hi")
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
		if stop != nil {
			select {
			case <-stop:
				return ErrStopped
			default:
			}
		}
	}
	flush()
	if err := sc.Err(); err != nil {
		return err
	}
	return ErrStreamClosed
}
