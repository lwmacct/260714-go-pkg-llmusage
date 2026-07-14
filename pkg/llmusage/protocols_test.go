package llmusage

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestSupportedProtocolFixtures(t *testing.T) {
	tests := []struct {
		name     string
		protocol Protocol
		format   Format
		file     string
		want     Result
	}{
		{"openai chat json", ProtocolOpenAIChatCompletions, FormatJSON, "testdata/openai-chat-completions/basic.json", Result{Protocol: ProtocolOpenAIChatCompletions, ResponseID: "chatcmpl_basic", Model: "gpt-5.4", Usage: Usage{InputTokens: 82, OutputTokens: 17, TotalTokens: 99, CachedInputTokens: 32, ReasoningTokens: 8}, TotalSource: TotalReported, Sequence: 1}},
		{"openai chat sse", ProtocolOpenAIChatCompletions, FormatSSE, "testdata/openai-chat-completions/basic.sse", Result{Protocol: ProtocolOpenAIChatCompletions, ResponseID: "chatcmpl_stream", Model: "gpt-5.4", Usage: Usage{InputTokens: 9, OutputTokens: 4, TotalTokens: 13, CachedInputTokens: 3, ReasoningTokens: 2}, TotalSource: TotalReported, Sequence: 3}},
		{"anthropic json", ProtocolAnthropicMessages, FormatJSON, "testdata/anthropic-messages/basic.json", Result{Protocol: ProtocolAnthropicMessages, ResponseID: "msg_basic", Model: "claude-sonnet-4-5", Usage: Usage{InputTokens: 25, OutputTokens: 15, CachedInputTokens: 10, CacheWriteTokens: 5}, TotalSource: TotalUnknown, Sequence: 1}},
		{"anthropic sse", ProtocolAnthropicMessages, FormatSSE, "testdata/anthropic-messages/basic.sse", Result{Protocol: ProtocolAnthropicMessages, ResponseID: "msg_stream", Model: "claude-sonnet-4-5", Usage: Usage{InputTokens: 12, OutputTokens: 9, CachedInputTokens: 7, CacheWriteTokens: 3}, TotalSource: TotalUnknown, Sequence: 4}},
		{"google json", ProtocolGoogleGenerateContent, FormatJSON, "testdata/google-generate-content/basic.json", Result{Protocol: ProtocolGoogleGenerateContent, ResponseID: "google_basic", Model: "gemini-2.5-pro", Usage: Usage{InputTokens: 30, OutputTokens: 20, TotalTokens: 55, CachedInputTokens: 6, ReasoningTokens: 5}, TotalSource: TotalReported, Sequence: 1}},
		{"google sse", ProtocolGoogleGenerateContent, FormatSSE, "testdata/google-generate-content/basic.sse", Result{Protocol: ProtocolGoogleGenerateContent, ResponseID: "google_stream", Model: "gemini-2.5-pro", Usage: Usage{InputTokens: 20, OutputTokens: 8, TotalTokens: 31, CachedInputTokens: 4, ReasoningTokens: 3}, TotalSource: TotalReported, Sequence: 2}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := readFixture(t, test.file)
			results, err := Parse(data, Options{Protocol: test.protocol, Format: test.format})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 {
				t.Fatalf("results=%#v", results)
			}
			results[0].RawUsage = nil
			if !reflect.DeepEqual(results[0], test.want) {
				t.Fatalf("got %#v want %#v", results[0], test.want)
			}
		})
	}
}

func TestEveryBoundaryForEveryProtocol(t *testing.T) {
	tests := []struct {
		protocol Protocol
		format   Format
		file     string
	}{
		{ProtocolOpenAIChatCompletions, FormatJSON, "testdata/openai-chat-completions/basic.json"},
		{ProtocolOpenAIChatCompletions, FormatSSE, "testdata/openai-chat-completions/basic.sse"},
		{ProtocolAnthropicMessages, FormatJSON, "testdata/anthropic-messages/basic.json"},
		{ProtocolAnthropicMessages, FormatSSE, "testdata/anthropic-messages/basic.sse"},
		{ProtocolGoogleGenerateContent, FormatJSON, "testdata/google-generate-content/basic.json"},
		{ProtocolGoogleGenerateContent, FormatSSE, "testdata/google-generate-content/basic.sse"},
	}
	for _, test := range tests {
		data := readFixture(t, test.file)
		want, err := Parse(data, Options{Protocol: test.protocol, Format: test.format})
		if err != nil {
			t.Fatal(err)
		}
		for split := 0; split <= len(data); split++ {
			decoder, err := NewDecoder(Options{Protocol: test.protocol, Format: test.format})
			if err != nil {
				t.Fatal(err)
			}
			var got []Result
			for _, chunk := range [][]byte{data[:split], data[split:]} {
				part, err := decoder.Feed(chunk)
				if err != nil {
					t.Fatalf("%s split %d: %v", test.file, split, err)
				}
				got = append(got, part...)
			}
			part, err := decoder.Finish()
			if err != nil {
				t.Fatalf("%s split %d finish: %v", test.file, split, err)
			}
			got = append(got, part...)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s split %d got %#v want %#v", test.file, split, got, want)
			}
		}
	}
}

func TestProtocolAuto(t *testing.T) {
	tests := []struct {
		protocol Protocol
		format   Format
		file     string
	}{
		{ProtocolOpenAIResponses, FormatJSON, "testdata/openai-responses/basic.json"},
		{ProtocolOpenAIResponses, FormatSSE, "testdata/openai-responses/basic.sse"},
		{ProtocolOpenAIChatCompletions, FormatJSON, "testdata/openai-chat-completions/basic.json"},
		{ProtocolOpenAIChatCompletions, FormatSSE, "testdata/openai-chat-completions/basic.sse"},
		{ProtocolAnthropicMessages, FormatJSON, "testdata/anthropic-messages/basic.json"},
		{ProtocolAnthropicMessages, FormatSSE, "testdata/anthropic-messages/basic.sse"},
		{ProtocolGoogleGenerateContent, FormatJSON, "testdata/google-generate-content/basic.json"},
		{ProtocolGoogleGenerateContent, FormatSSE, "testdata/google-generate-content/basic.sse"},
	}
	for _, test := range tests {
		data := readFixture(t, test.file)
		results, err := Parse(data, Options{Protocol: ProtocolAuto, Format: test.format})
		if err != nil {
			t.Fatalf("%s: %v", test.file, err)
		}
		if len(results) != 1 || results[0].Protocol != test.protocol {
			t.Fatalf("%s: %#v", test.file, results)
		}
	}
	for _, input := range []struct {
		format Format
		data   string
	}{{FormatJSON, `{}`}, {FormatSSE, "data: {}\n\n"}} {
		if _, err := Parse([]byte(input.data), Options{Protocol: ProtocolAuto, Format: input.format}); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("expected unsupported auto payload, got %v", err)
		}
	}
}

func TestProtocolSpecificSemantics(t *testing.T) {
	results, err := Parse([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"usage\":null}\n\ndata: [DONE]\n\n"), Options{Protocol: ProtocolOpenAIChatCompletions, Format: FormatSSE})
	if err != nil || len(results) != 0 {
		t.Fatalf("chat without final usage: results=%#v err=%v", results, err)
	}

	results, err = Parse([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"c\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":2}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\n"), Options{Protocol: ProtocolAnthropicMessages, Format: FormatSSE})
	if err != nil || len(results) != 1 || results[0].Usage.OutputTokens != 3 {
		t.Fatalf("anthropic cumulative merge: %#v %v", results, err)
	}

	results, err = Parse([]byte("data: {\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":2,\"totalTokenCount\":3}}\n\ndata: {\"usageMetadata\":{\"promptTokenCount\":4,\"candidatesTokenCount\":5,\"totalTokenCount\":9}}\n\n"), Options{Protocol: ProtocolGoogleGenerateContent, Format: FormatSSE})
	if err != nil || len(results) != 1 || results[0].Usage.TotalTokens != 9 {
		t.Fatalf("google latest snapshot: %#v %v", results, err)
	}
}

func TestUnknownSSEEventsAreIgnored(t *testing.T) {
	tests := []Protocol{ProtocolOpenAIResponses, ProtocolOpenAIChatCompletions, ProtocolAnthropicMessages, ProtocolGoogleGenerateContent}
	for _, protocol := range tests {
		results, err := Parse([]byte("event: future.event\ndata: not-json\n\n"), Options{Protocol: protocol, Format: FormatSSE})
		if err != nil || len(results) != 0 {
			t.Fatalf("%s unknown event: results=%#v err=%v", protocol, results, err)
		}
	}
}

func TestRawUsagePreservesExtensions(t *testing.T) {
	for _, fixture := range []struct {
		protocol  Protocol
		format    Format
		file, key string
	}{
		{ProtocolOpenAIChatCompletions, FormatJSON, "testdata/openai-chat-completions/basic.json", "provider_extension"},
		{ProtocolAnthropicMessages, FormatJSON, "testdata/anthropic-messages/basic.json", "server_tool_use"},
		{ProtocolGoogleGenerateContent, FormatJSON, "testdata/google-generate-content/basic.json", "promptTokensDetails"},
	} {
		results, err := Parse(readFixture(t, fixture.file), Options{Protocol: fixture.protocol, Format: fixture.format})
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]any
		if err := json.Unmarshal(results[0].RawUsage, &raw); err != nil {
			t.Fatal(err)
		}
		if _, ok := raw[fixture.key]; !ok {
			t.Fatalf("%s lost %s: %s", fixture.file, fixture.key, results[0].RawUsage)
		}
	}
}

func TestAllProtocolsRejectInvalidCounters(t *testing.T) {
	tests := []struct {
		protocol Protocol
		data     string
	}{
		{ProtocolOpenAIChatCompletions, `{"object":"chat.completion","usage":{"prompt_tokens":-1}}`},
		{ProtocolAnthropicMessages, `{"type":"message","usage":{"input_tokens":1.5}}`},
		{ProtocolGoogleGenerateContent, `{"usageMetadata":{"totalTokenCount":9223372036854775808}}`},
	}
	for _, test := range tests {
		if _, err := Parse([]byte(test.data), Options{Protocol: test.protocol, Format: FormatJSON}); !errors.Is(err, ErrMalformedStream) {
			t.Fatalf("%s expected malformed counter, got %v", test.protocol, err)
		}
	}
}

func TestAllProtocolsDoNotRetainInputBuffer(t *testing.T) {
	tests := []struct {
		protocol Protocol
		file     string
	}{
		{ProtocolOpenAIChatCompletions, "testdata/openai-chat-completions/basic.sse"},
		{ProtocolAnthropicMessages, "testdata/anthropic-messages/basic.sse"},
		{ProtocolGoogleGenerateContent, "testdata/google-generate-content/basic.sse"},
	}
	for _, test := range tests {
		data := readFixture(t, test.file)
		decoder, err := NewDecoder(Options{Protocol: test.protocol, Format: FormatSSE})
		if err != nil {
			t.Fatal(err)
		}
		var results []Result
		for start := 0; start < len(data); start += 11 {
			end := min(start+11, len(data))
			buffer := append([]byte(nil), data[start:end]...)
			part, err := decoder.Feed(buffer)
			if err != nil {
				t.Fatalf("%s: %v", test.protocol, err)
			}
			results = append(results, part...)
			clear(buffer)
		}
		part, err := decoder.Finish()
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, part...)
		if len(results) != 1 || results[0].Protocol != test.protocol {
			t.Fatalf("%s: %#v", test.protocol, results)
		}
	}
}

func TestProtocolAutoRejectsAmbiguousJSON(t *testing.T) {
	data := []byte(`{"object":"response","usage":{"input_tokens":1,"output_tokens":1},"usageMetadata":{"promptTokenCount":1}}`)
	if _, err := Parse(data, Options{Protocol: ProtocolAuto, Format: FormatJSON}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ambiguous auto payload to be unsupported, got %v", err)
	}
}

func TestMergedAnthropicUsageHonorsResultLimit(t *testing.T) {
	stream := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"c\",\"usage\":{\"input_tokens\":1}}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":2,\"extension\":\"abcdefghijklmnopqrstuvwxyz\"}}\n\n")
	_, err := Parse(stream, Options{Protocol: ProtocolAnthropicMessages, Format: FormatSSE, MaxResultBytes: 48})
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("expected merged usage limit, got %v", err)
	}
}

func FuzzProtocolAuto(f *testing.F) {
	f.Add([]byte(`{"object":"chat.completion","usage":null}`), uint8(0))
	f.Add([]byte("data: {\"usageMetadata\":{\"totalTokenCount\":1}}\n\n"), uint8(1))
	f.Fuzz(func(t *testing.T, data []byte, formatByte uint8) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		format := FormatJSON
		if formatByte%2 == 1 {
			format = FormatSSE
		}
		decoder, err := NewDecoder(Options{Protocol: ProtocolAuto, Format: format, MaxFrameBytes: 4096, MaxResultBytes: 4096})
		if err != nil {
			t.Fatal(err)
		}
		for len(data) > 0 {
			size := min(13, len(data))
			if _, err := decoder.Feed(data[:size]); err != nil {
				return
			}
			data = data[size:]
		}
		_, _ = decoder.Finish()
	})
}
