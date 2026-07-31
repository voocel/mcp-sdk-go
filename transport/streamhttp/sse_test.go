package streamhttp

import (
	"bytes"
	"strings"
	"testing"
)

func TestSSERoundTrip(t *testing.T) {
	events := []event{
		{Data: []byte(`{"a":1}`)},
		{Name: "message", Data: []byte("line1\nline2")},
		{Data: []byte("")},
	}
	var buf bytes.Buffer
	for _, e := range events {
		if err := writeSSE(&buf, e); err != nil {
			t.Fatal(err)
		}
	}

	var got []event
	scanSSE(&buf, func(e event, err error) bool {
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, e)
		return true
	})
	// The empty event is not round-tripped (nothing to deliver).
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2", len(got))
	}
	if string(got[0].Data) != `{"a":1}` {
		t.Fatalf("data[0] = %q", got[0].Data)
	}
	if got[1].Name != "message" || string(got[1].Data) != "line1\nline2" {
		t.Fatalf("event[1] = %+v", got[1])
	}
}

func TestScanSSEIgnoresCommentsAndIDs(t *testing.T) {
	in := ": keepalive\nid: 42\nretry: 1000\ndata: hello\n\n"
	var got []event
	scanSSE(strings.NewReader(in), func(e event, err error) bool {
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, e)
		return true
	})
	if len(got) != 1 || string(got[0].Data) != "hello" {
		t.Fatalf("got %+v", got)
	}
}

func TestScanSSEBareFieldLine(t *testing.T) {
	// A line without a colon is a field with an empty value per the SSE spec —
	// never a parse error. Unknown fields are ignored, and a bare "data" event
	// with an empty buffer dispatches nothing.
	in := "unknown-field\ndata: hello\n\ndata\n\n"
	var got []event
	scanSSE(strings.NewReader(in), func(e event, err error) bool {
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, e)
		return true
	})
	if len(got) != 1 || string(got[0].Data) != "hello" {
		t.Fatalf("got %+v", got)
	}
}

func TestScanSSEInterleavedFields(t *testing.T) {
	// id/retry/unknown fields between data lines must not disturb the
	// accumulating data buffer (regression: flushing on non-data fields
	// truncated multi-line data).
	in := "data: a\nid: 1\ndata: b\nretry: 100\ndata: c\n\n"
	var got []event
	scanSSE(strings.NewReader(in), func(e event, err error) bool {
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, e)
		return true
	})
	if len(got) != 1 || string(got[0].Data) != "a\nb\nc" {
		t.Fatalf("got %+v, want one event with data \"a\\nb\\nc\"", got)
	}
}

func TestSSELargePayload(t *testing.T) {
	// A whole JSON-RPC message travels on one data line; the scanner must
	// accept lines far beyond the old 1 MiB cap.
	big := event{Data: []byte(`{"pad":"` + strings.Repeat("x", 2<<20) + `"}`)}
	var buf bytes.Buffer
	if err := writeSSE(&buf, big); err != nil {
		t.Fatal(err)
	}
	var got []event
	scanSSE(&buf, func(e event, err error) bool {
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, e)
		return true
	})
	if len(got) != 1 || len(got[0].Data) != len(big.Data) {
		t.Fatalf("large payload lost: %d events", len(got))
	}
}

func TestScanSSEFinalEventWithoutBlankLine(t *testing.T) {
	var got []event
	scanSSE(strings.NewReader("data: tail"), func(e event, err error) bool {
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, e)
		return true
	})
	if len(got) != 1 || string(got[0].Data) != "tail" {
		t.Fatalf("got %+v", got)
	}
}
