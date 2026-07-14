package llmusage

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestParseOpenAIResponsesJSON(t *testing.T) {
	data := readFixture(t, "testdata/openai-responses/basic.json")
	results, err := Parse(data, Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %#v", results)
	}
	want := Usage{InputTokens: 81, OutputTokens: 1035, TotalTokens: 1116, CachedInputTokens: 32, CacheWriteTokens: 7, ReasoningTokens: 832}
	if results[0].ResponseID != "resp_basic" || results[0].Model != "gpt-5.4" || results[0].Protocol != ProtocolOpenAIResponses || results[0].TotalSource != TotalReported || results[0].Sequence != 1 || results[0].Usage != want {
		t.Fatalf("unexpected result: %#v", results[0])
	}
	assertRawUsageFields(t, results[0].RawUsage)
}

func TestDecoderOpenAIResponsesSSEAcrossEveryBoundary(t *testing.T) {
	data := readFixture(t, "testdata/openai-responses/basic.sse")
	want, err := Parse(data, Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE})
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 1 || want[0].ResponseID != "resp_stream" || want[0].Usage.CachedInputTokens != 11 || want[0].Usage.CacheWriteTokens != 3 {
		t.Fatalf("unexpected baseline: %#v", want)
	}

	for split := 0; split <= len(data); split++ {
		decoder, newErr := NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE})
		if newErr != nil {
			t.Fatal(newErr)
		}
		var got []Result
		for _, chunk := range [][]byte{data[:split], data[split:]} {
			results, feedErr := decoder.Feed(chunk)
			if feedErr != nil {
				t.Fatalf("split %d: %v", split, feedErr)
			}
			got = append(got, results...)
		}
		results, finishErr := decoder.Finish()
		if finishErr != nil {
			t.Fatalf("split %d finish: %v", split, finishErr)
		}
		got = append(got, results...)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("split %d: got %#v want %#v", split, got, want)
		}
	}
}

func TestDecoderOpenAIResponsesJSONAcrossEveryBoundary(t *testing.T) {
	data := readFixture(t, "testdata/openai-responses/basic.json")
	want, err := Parse(data, Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}
	for split := 0; split <= len(data); split++ {
		decoder, newErr := NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON})
		if newErr != nil {
			t.Fatal(newErr)
		}
		if _, feedErr := decoder.Feed(data[:split]); feedErr != nil {
			t.Fatalf("split %d: %v", split, feedErr)
		}
		if _, feedErr := decoder.Feed(data[split:]); feedErr != nil {
			t.Fatalf("split %d: %v", split, feedErr)
		}
		got, finishErr := decoder.Finish()
		if finishErr != nil {
			t.Fatalf("split %d finish: %v", split, finishErr)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("split %d: got %#v want %#v", split, got, want)
		}
	}
}

func TestDecoderLifecycleAndOptions(t *testing.T) {
	if _, err := NewDecoder(Options{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("expected invalid options, got %v", err)
	}
	if _, err := NewDecoder(Options{Protocol: ProtocolAuto, Format: FormatSSE}); err != nil {
		t.Fatalf("expected auto protocol support, got %v", err)
	}
	decoder, err := NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decoder.Finish(); err != nil {
		t.Fatal(err)
	}
	if results, err := decoder.Finish(); err != nil || len(results) != 0 {
		t.Fatalf("repeated finish should be empty: results=%#v err=%v", results, err)
	}
	if _, err = decoder.Feed([]byte("{}")); !errors.Is(err, ErrFinished) {
		t.Fatalf("expected finished error, got %v", err)
	}
}

func TestDefaultResourceLimits(t *testing.T) {
	options, err := normalizeOptions(Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE})
	if err != nil {
		t.Fatal(err)
	}
	if options.MaxSSEMetadataBytes != 64<<10 || options.MaxResultBytes != 64<<10 || options.MaxNestingDepth != 128 {
		t.Fatalf("unexpected defaults: %#v", options)
	}
	if _, err := NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE, MaxSSEMetadataBytes: -1}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("expected invalid metadata limit, got %v", err)
	}
}

func TestParseDerivesMissingTotalAndSkipsNullUsage(t *testing.T) {
	data := []byte(`{"id":"resp_derived","model":"model","usage":{"input_tokens":2,"output_tokens":3}}`)
	results, err := Parse(data, Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Usage.TotalTokens != 5 || results[0].TotalSource != TotalDerived {
		t.Fatalf("unexpected derived result: %#v", results)
	}
	results, err = Parse([]byte(`{"id":"resp_null","usage":null}`), Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON})
	if err != nil || len(results) != 0 {
		t.Fatalf("null usage should be skipped: results=%#v err=%v", results, err)
	}
}

func TestParseMillionTokenUsage(t *testing.T) {
	data := []byte(`{"object":"response","usage":{"input_tokens":1000000,"output_tokens":250000,"total_tokens":1250000}}`)
	results, err := Parse(data, Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}
	want := Usage{InputTokens: 1000000, OutputTokens: 250000, TotalTokens: 1250000}
	if len(results) != 1 || results[0].Usage != want {
		t.Fatalf("unexpected usage: %#v", results)
	}
}

func TestParseRejectsInvalidUsageNumbers(t *testing.T) {
	for _, raw := range []string{
		`{"usage":{"input_tokens":-1,"output_tokens":2,"total_tokens":1}}`,
		`{"usage":{"input_tokens":1.5,"output_tokens":2,"total_tokens":3}}`,
		`{"usage":{"input_tokens":9223372036854775808,"output_tokens":2,"total_tokens":3}}`,
	} {
		_, err := Parse([]byte(raw), Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON})
		if !errors.Is(err, ErrMalformedStream) {
			t.Fatalf("expected malformed stream for %s, got %v", raw, err)
		}
	}
}

func TestDecoderEmitsMultipleCompletedEventsWithWireSequence(t *testing.T) {
	stream := []byte(`event: response.created
data: {"type":"response.created","response":{"usage":null}}

data: {"type":"response.completed","response":{"id":"one","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}

event: response.completed
data: {"type":"response.completed","response":{"id":"two","usage":{"input_tokens":4,"output_tokens":5,"total_tokens":9}}}

`)
	results, err := Parse(stream, Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].ResponseID != "one" || results[0].Sequence != 2 || results[1].ResponseID != "two" || results[1].Sequence != 3 {
		t.Fatalf("unexpected results: %#v", results)
	}
}

func TestDecoderLimitsRetainedUsageButSkipsLargeOutput(t *testing.T) {
	large := make([]byte, 2<<20)
	for index := range large {
		large[index] = 'x'
	}
	jsonData := append([]byte(`{"id":"large","output":"`), large...)
	jsonData = append(jsonData, []byte(`","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`)...)
	results, err := Parse(jsonData, Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON, MaxResultBytes: 256})
	if err != nil || len(results) != 1 {
		t.Fatalf("large skipped output failed: results=%#v err=%v", results, err)
	}
	_, err = Parse(jsonData, Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON, MaxResultBytes: 16})
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("expected retained result limit, got %v", err)
	}
}

func TestDecoderAcceptsSSEFieldOrderAndEOFWithoutBlankLine(t *testing.T) {
	stream := []byte(`data: {"type":"response.completed","response":{"id":"ordered","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}
event: response.completed`)
	results, err := Parse(stream, Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE})
	if err != nil || len(results) != 1 || results[0].ResponseID != "ordered" {
		t.Fatalf("unexpected EOF result: results=%#v err=%v", results, err)
	}
}

func TestDecoderRecognizesCompletedTypeAfterResponseLimit(t *testing.T) {
	large := make([]byte, 512)
	for index := range large {
		large[index] = 'x'
	}
	stream := append([]byte(`data: {"response":{"usage":{"unknown":"`), large...)
	stream = append(stream, []byte(`"}},"type":"response.completed"}

`)...)
	_, err := Parse(stream, Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE, MaxSSEMetadataBytes: 1024, MaxResultBytes: 64})
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("completed type after oversized usage should preserve error, got %v", err)
	}
}

func TestDecoderSharesResultBudgetAcrossSSEScanners(t *testing.T) {
	stream := []byte(`event: response.completed
data: {"type":"response.completed","response":{"usage":{"input_tokens":1}}}

`)
	retainedBytes := len(`"response.completed"`) + len(`{"input_tokens":1}`)
	_, err := Parse(stream, Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE, MaxResultBytes: retainedBytes - 1})
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("expected shared result budget limit, got %v", err)
	}
}

func TestDecoderRejectsMalformedTargetEventWithoutEventField(t *testing.T) {
	stream := []byte(`data: {"type":"response.completed","response":{"usage":{"input_tokens":1,}}}

`)
	_, err := Parse(stream, Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE})
	if !errors.Is(err, ErrMalformedStream) {
		t.Fatalf("expected malformed completed event, got %v", err)
	}
}

func TestDecoderDoesNotRetainInputBuffer(t *testing.T) {
	data := readFixture(t, "testdata/openai-responses/basic.sse")
	decoder, err := NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE})
	if err != nil {
		t.Fatal(err)
	}
	var results []Result
	for start := 0; start < len(data); start += 17 {
		end := min(start+17, len(data))
		buffer := append([]byte(nil), data[start:end]...)
		part, feedErr := decoder.Feed(buffer)
		if feedErr != nil {
			t.Fatal(feedErr)
		}
		results = append(results, part...)
		clear(buffer)
	}
	part, err := decoder.Finish()
	if err != nil {
		t.Fatal(err)
	}
	results = append(results, part...)
	if len(results) != 1 || results[0].ResponseID != "resp_stream" {
		t.Fatalf("unexpected results after buffer reuse: %#v", results)
	}
}

func assertRawUsageFields(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var usage map[string]any
	if err := json.Unmarshal(raw, &usage); err != nil {
		t.Fatal(err)
	}
	if _, ok := usage["future_usage"].(map[string]any); !ok {
		t.Fatalf("raw usage lost unknown fields: %s", raw)
	}
	details, _ := usage["input_tokens_details"].(map[string]any)
	if _, ok := details["future_detail"]; !ok {
		t.Fatalf("raw usage lost unknown detail: %s", raw)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func FuzzDecoder(f *testing.F) {
	f.Add([]byte(`event: response.completed
data: {"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}

`), uint8(7))
	f.Add([]byte(`data: {}

`), uint8(1))
	f.Fuzz(func(t *testing.T, data []byte, chunkSize uint8) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		decoder, err := NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE, MaxSSEMetadataBytes: 4096, MaxResultBytes: 4096})
		if err != nil {
			t.Fatal(err)
		}
		size := int(chunkSize) + 1
		for len(data) > 0 {
			n := min(size, len(data))
			if _, err = decoder.Feed(data[:n]); err != nil {
				return
			}
			data = data[n:]
		}
		_, _ = decoder.Finish()
	})
}

func BenchmarkDecoderOpenAIResponsesSSELargeOutput(b *testing.B) {
	large := make([]byte, 4<<20)
	for index := range large {
		large[index] = 'x'
	}
	stream := append([]byte(`event: response.completed
data: {"type":"response.completed","response":{"id":"bench","output":"`), large...)
	stream = append(stream, []byte(`","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}

`)...)
	b.ReportAllocs()
	b.SetBytes(int64(len(stream)))
	b.ResetTimer()
	for b.Loop() {
		results, err := Parse(stream, Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE})
		if err != nil || len(results) != 1 {
			b.Fatalf("results=%d err=%v", len(results), err)
		}
	}
}
