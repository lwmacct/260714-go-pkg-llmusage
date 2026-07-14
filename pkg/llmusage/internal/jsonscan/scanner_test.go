package jsonscan

import (
	"errors"
	"reflect"
	"testing"
)

func TestScannerCapturesRootAndNestedObjectAcrossEveryBoundary(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		path []string
		want map[string]string
	}{
		{name: "root", raw: `{"id":"root","skip":{"id":"nested"},"usage":{"total":3}}`, want: map[string]string{"id": `"root"`, "usage": `{"total":3}`}},
		{name: "nested", raw: `{"other":{"response":{"id":"wrong"}},"response":{"id":"right","skip":[1,2],"usage":{"total":4}}}`, path: []string{"response"}, want: map[string]string{"id": `"right"`, "usage": `{"total":4}`}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for split := 0; split <= len(test.raw); split++ {
				scanner := NewScanner(Options{ObjectPath: test.path, Fields: []string{"id", "usage"}, MaxBytes: 1024})
				if err := scanner.Write([]byte(test.raw[:split])); err != nil {
					t.Fatalf("split %d: %v", split, err)
				}
				if err := scanner.Write([]byte(test.raw[split:])); err != nil {
					t.Fatalf("split %d: %v", split, err)
				}
				result, err := scanner.Finish()
				if err != nil {
					t.Fatalf("split %d finish: %v", split, err)
				}
				got := make(map[string]string, len(result.Fields))
				for key, value := range result.Fields {
					got[key] = string(value)
				}
				if !result.Found || !reflect.DeepEqual(got, test.want) {
					t.Fatalf("split %d: found=%t got=%#v", split, result.Found, got)
				}
			}
		})
	}
}

func TestScannerRejectsMalformedCapturedValueAndLimits(t *testing.T) {
	scanner := NewScanner(Options{Fields: []string{"usage"}, MaxBytes: 1024})
	if err := scanner.Write([]byte(`{"usage":{"total":`)); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.Finish(); !errors.Is(err, ErrMalformed) {
		t.Fatalf("expected malformed error, got %v", err)
	}

	scanner = NewScanner(Options{Fields: []string{"usage"}, MaxBytes: 4})
	if err := scanner.Write([]byte(`{"usage":{"total":3}}`)); !errors.Is(err, ErrLimit) {
		t.Fatalf("expected limit error, got %v", err)
	}
}

func TestScannerRejectsMalformedSkippedValues(t *testing.T) {
	for _, raw := range []string{
		`{"skip":truX,"usage":{"total":3}}`,
		`{"skip":01,"usage":{"total":3}}`,
		`{"skip":1e,"usage":{"total":3}}`,
		`{"skip":-.1,"usage":{"total":3}}`,
		`{"skip":"bad\q","usage":{"total":3}}`,
		`{"skip":"bad\u12xz","usage":{"total":3}}`,
		"{\"skip\":\"bad\nvalue\",\"usage\":{\"total\":3}}",
		`{"skip":[1,],"usage":{"total":3}}`,
		`{"skip":[1 2],"usage":{"total":3}}`,
		`{"skip":{"a":1,},"usage":{"total":3}}`,
		`{{"skip":1}}`,
		`{[1,2]:3}`,
	} {
		scanner := NewScanner(Options{Fields: []string{"usage"}, MaxBytes: 1024})
		writeErr := scanner.Write([]byte(raw))
		if writeErr == nil {
			_, writeErr = scanner.Finish()
		}
		if !errors.Is(writeErr, ErrMalformed) {
			t.Fatalf("expected malformed error for %q, got %v", raw, writeErr)
		}
	}
}

func TestScannerBoundsNestingDepth(t *testing.T) {
	scanner := NewScanner(Options{Fields: []string{"usage"}, MaxBytes: 1024, MaxDepth: 3})
	if err := scanner.Write([]byte(`{"skip":[[[[]]]],"usage":{"total":3}}`)); !errors.Is(err, ErrLimit) {
		t.Fatalf("expected depth limit, got %v", err)
	}
	scanner = NewScanner(Options{Fields: []string{"usage"}, MaxBytes: 1024, MaxDepth: 3})
	if err := scanner.Write([]byte(`{"usage":{"detail":{"nested":{}}}}`)); !errors.Is(err, ErrLimit) {
		t.Fatalf("expected captured depth limit, got %v", err)
	}
}

func TestScannerSkipsLargeUnselectedValue(t *testing.T) {
	large := make([]byte, 8<<20)
	for index := range large {
		large[index] = 'x'
	}
	raw := append([]byte(`{"output":"`), large...)
	raw = append(raw, []byte(`","usage":{"total":3}}`)...)
	scanner := NewScanner(Options{Fields: []string{"usage"}, MaxBytes: 64})
	if err := scanner.Write(raw); err != nil {
		t.Fatal(err)
	}
	result, err := scanner.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Fields["usage"]) != `{"total":3}` || cap(scanner.valueBuf) > 64 || cap(scanner.stringBuf) > 64 {
		t.Fatalf("large skipped value was retained: result=%#v value_cap=%d key_cap=%d", result, cap(scanner.valueBuf), cap(scanner.stringBuf))
	}
}

func FuzzScanner(f *testing.F) {
	f.Add([]byte(`{"id":"resp","usage":{"input_tokens":1}}`))
	f.Add([]byte(`{"response":{"usage":null}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		scanner := NewScanner(Options{ObjectPath: []string{"response"}, Fields: []string{"id", "model", "usage"}, MaxBytes: 4096})
		_ = scanner.Write(data)
		_, _ = scanner.Finish()
	})
}
