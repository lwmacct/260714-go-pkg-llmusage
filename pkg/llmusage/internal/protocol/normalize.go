package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/jsonscan"
)

func newScanner(path, fields []string, limits Limits) *jsonscan.Scanner {
	return jsonscan.NewScanner(jsonscan.Options{
		ObjectPath: path,
		Fields:     fields,
		MaxBytes:   limits.MaxResultBytes,
		MaxDepth:   limits.MaxNestingDepth,
	})
}

func isNull(raw json.RawMessage) bool {
	return len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func requireObject(name string, raw json.RawMessage) error {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 || value[0] != '{' || !json.Valid(value) {
		return fmt.Errorf("%s must be an object", name)
	}
	return nil
}

func parseCounter(name string, raw json.RawMessage) (int64, bool, error) {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return 0, false, nil
	}
	parsed, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil || parsed < 0 {
		return 0, true, fmt.Errorf("invalid %s", name)
	}
	return parsed, true, nil
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func totalFromCounters(input int64, inputPresent bool, output int64, outputPresent bool, total int64, totalPresent bool) (int64, TotalSource, error) {
	if totalPresent {
		return total, TotalReported, nil
	}
	if !inputPresent || !outputPresent {
		return 0, TotalUnknown, nil
	}
	if input > int64(^uint64(0)>>1)-output {
		return 0, TotalUnknown, fmt.Errorf("derived total tokens overflows int64")
	}
	return input + output, TotalDerived, nil
}

func resultLimit(id, model string, raw json.RawMessage, limits Limits) error {
	if len(id)+len(model)+len(raw) > limits.MaxResultBytes {
		return jsonscan.ErrLimit
	}
	return nil
}
