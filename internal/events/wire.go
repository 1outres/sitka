package events

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// eventName is the name every routing frame carries.
const eventName = "request"

// maxFrameSize bounds one frame a decoder reads.
const maxFrameSize = 1 << 20

// flusher is what an HTTP response writer offers to push bytes out at once.
type flusher interface{ Flush() }

// Encoder writes events as a server-sent event stream.
type Encoder struct {
	w io.Writer
}

// NewEncoder writes to w, flushing after every frame when w can flush.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// Encode writes one event.
func (e *Encoder) Encode(event Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("events: encode an event: %w", err)
	}

	var frame bytes.Buffer
	frame.WriteString("event: ")
	frame.WriteString(eventName)
	frame.WriteString("\ndata: ")
	frame.Write(payload)
	frame.WriteString("\n\n")

	return e.write(frame.Bytes())
}

// Ping writes a comment frame. It opens the stream before the first event and
// tells the writer once the reader has gone away.
func (e *Encoder) Ping() error {
	return e.write([]byte(": ping\n\n"))
}

func (e *Encoder) write(frame []byte) error {
	if _, err := e.w.Write(frame); err != nil {
		return fmt.Errorf("events: write the event stream: %w", err)
	}
	if f, ok := e.w.(flusher); ok {
		f.Flush()
	}
	return nil
}

// Decoder reads an event stream written by an Encoder.
type Decoder struct {
	scanner *bufio.Scanner
}

// NewDecoder reads frames from r.
func NewDecoder(r io.Reader) *Decoder {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4<<10), maxFrameSize)
	return &Decoder{scanner: scanner}
}

// Next returns the next event, skipping comments and blank lines. It returns
// io.EOF once the stream ends.
func (d *Decoder) Next() (Event, error) {
	for d.scanner.Scan() {
		line := bytes.TrimRight(d.scanner.Bytes(), "\r")
		payload, ok := bytes.CutPrefix(line, []byte("data:"))
		if !ok {
			continue
		}
		var event Event
		if err := json.Unmarshal(bytes.TrimSpace(payload), &event); err != nil {
			return Event{}, fmt.Errorf("events: decode an event: %w", err)
		}
		return event, nil
	}
	if err := d.scanner.Err(); err != nil {
		return Event{}, fmt.Errorf("events: read the event stream: %w", err)
	}
	return Event{}, io.EOF
}
