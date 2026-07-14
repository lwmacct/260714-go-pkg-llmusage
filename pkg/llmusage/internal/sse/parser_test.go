package sse

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParserHandlesSSEGrammarAcrossEveryBoundary(t *testing.T) {
	input := []byte("\xef\xbb\xbf: hello\r\nid: 7\revent: update\ndata: first\r\ndata: second\r\nretry: 1500\r\n\r\ndata\n\n")
	wantData := []string{"first\nsecond", ""}

	for split := 0; split <= len(input); split++ {
		var current []byte
		var data []string
		var events []Event
		parser := NewParser(1024, func(chunk []byte) error {
			current = append(current, chunk...)
			return nil
		}, func(event Event) error {
			data = append(data, string(current))
			current = nil
			events = append(events, event)
			return nil
		})
		if err := parser.Feed(input[:split]); err != nil {
			t.Fatalf("split %d: %v", split, err)
		}
		if err := parser.Feed(input[split:]); err != nil {
			t.Fatalf("split %d: %v", split, err)
		}
		if err := parser.Finish(); err != nil {
			t.Fatalf("split %d finish: %v", split, err)
		}
		if !reflect.DeepEqual(data, wantData) {
			t.Fatalf("split %d data: %#v", split, data)
		}
		if len(events) != 2 || events[0].Sequence != 1 || events[0].Type != "update" || events[0].ID != "7" || events[0].RetryMillis == nil || *events[0].RetryMillis != 1500 || events[1].Type != "message" {
			t.Fatalf("split %d events: %#v", split, events)
		}
	}
}

func TestParserCarriesLastEventIDAndIgnoresNULID(t *testing.T) {
	var events []Event
	parser := NewParser(1024, nil, func(event Event) error {
		events = append(events, event)
		return nil
	})
	if err := parser.Feed([]byte("id: one\ndata: a\n\nid: bad\x00id\ndata: b\n\nid\ndata: c\n\n")); err != nil {
		t.Fatal(err)
	}
	if err := parser.Finish(); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].ID != "one" || events[1].ID != "one" || events[2].ID != "" {
		t.Fatalf("unexpected IDs: %#v", events)
	}
}

func TestParserEmptyColonFieldsClearEventTypeAndLastID(t *testing.T) {
	var events []Event
	parser := NewParser(1024, nil, func(event Event) error {
		events = append(events, event)
		return nil
	})
	input := []byte("id: one\nevent: update\nevent:\nid:\ndata: value\n\n")
	if err := parser.Feed(input); err != nil {
		t.Fatal(err)
	}
	if err := parser.Finish(); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "message" || events[0].ID != "" {
		t.Fatalf("unexpected event: %#v", events)
	}
}

func TestParserBoundsMetadata(t *testing.T) {
	parser := NewParser(10, nil, nil)
	if err := parser.Feed([]byte("event: abcde\n")); err != nil {
		t.Fatalf("exact metadata limit should pass, got %v", err)
	}
	parser = NewParser(9, nil, nil)
	if err := parser.Feed([]byte("event: abcde\n")); !errors.Is(err, ErrLimit) {
		t.Fatalf("expected metadata limit, got %v", err)
	}
}

func TestParserDoesNotCountDataAsMetadata(t *testing.T) {
	data := strings.Repeat("x", 1<<20)
	parser := NewParser(4, func([]byte) error { return nil }, func(Event) error { return nil })
	if err := parser.Feed([]byte("data: " + data + "\n\n")); err != nil {
		t.Fatalf("large data must not consume metadata budget: %v", err)
	}
}

func FuzzParser(f *testing.F) {
	f.Add([]byte("event: response.completed\ndata: {}\n\n"))
	f.Add([]byte("\xef\xbb\xbfdata: one\r\ndata: two\r\r"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		parser := NewParser(4096, func([]byte) error { return nil }, func(Event) error { return nil })
		_ = parser.Feed(data)
		_ = parser.Finish()
	})
}
