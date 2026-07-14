package protocol

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDetectJSONRequiresUniqueSignature(t *testing.T) {
	fields := map[string]json.RawMessage{
		"object":        []byte(`"response"`),
		"usageMetadata": []byte(`{}`),
	}
	if _, err := detectJSON(fields); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ambiguous signature, got %v", err)
	}
}

func TestDetectSSEUsesStrongSignatures(t *testing.T) {
	if _, matched := detectSSE("ping", nil); matched {
		t.Fatal("ping must not select Anthropic")
	}
	if kind, matched := detectSSE("message_start", nil); !matched || kind != AnthropicMessages {
		t.Fatalf("got kind=%s matched=%v", kind, matched)
	}
}
