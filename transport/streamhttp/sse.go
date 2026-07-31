package streamhttp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// event is one SSE frame. 2026-07-28 defines no event IDs or resumability, so
// only the name and data fields exist; id/retry fields from peers are ignored.
type event struct {
	Name string
	Data []byte
}

func writeSSE(w io.Writer, evt event) error {
	var b bytes.Buffer
	if evt.Name != "" {
		fmt.Fprintf(&b, "event: %s\n", evt.Name)
	}
	if len(evt.Data) == 0 {
		b.WriteString("data: \n\n")
	} else {
		for line := range bytes.SplitSeq(evt.Data, []byte("\n")) {
			fmt.Fprintf(&b, "data: %s\n", line)
		}
		b.WriteString("\n")
	}
	if _, err := w.Write(b.Bytes()); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

// maxSSELineBytes must accommodate a whole JSON-RPC message on one data line;
// it matches the transport's default response cap.
const maxSSELineBytes = 32 << 20

// scanSSE reads events until EOF, invoking handle for each. Returning false
// from handle stops the scan. Comment lines and unknown fields (id, retry)
// are ignored per the SSE spec; handle only receives an error for scanner
// failures (oversized line, I/O).
func scanSSE(r io.Reader, handle func(event, error) bool) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(nil, maxSSELineBytes)

	var (
		name    string
		dataBuf *bytes.Buffer
	)
	// dispatch settles the pending event at a blank line (or EOF). Per the SSE
	// spec an event whose data buffer is empty dispatches nothing.
	dispatch := func() bool {
		ok := true
		if dataBuf != nil && dataBuf.Len() > 0 {
			ok = handle(event{Name: name, Data: dataBuf.Bytes()}, nil)
		}
		name, dataBuf = "", nil
		return ok
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			if !dispatch() {
				return
			}
			continue
		}
		if line[0] == ':' {
			continue // keep-alive comment
		}
		// A line without a colon is a field name with an empty value per the
		// SSE spec — never a parse error.
		field, value, _ := bytes.Cut(line, []byte{':'})
		value = bytes.TrimPrefix(value, []byte(" "))
		switch string(field) {
		case "event":
			name = string(value)
		case "data":
			if dataBuf == nil {
				dataBuf = new(bytes.Buffer)
			} else {
				dataBuf.WriteByte('\n')
			}
			dataBuf.Write(value)
		default:
			// id, retry, unknown fields: ignored in this protocol revision.
			// Crucially they must not disturb an accumulating data buffer.
		}
	}
	if err := scanner.Err(); err != nil {
		handle(event{}, err)
		return
	}
	dispatch()
}
