package llmusage

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage/internal/jsoncapture"
)

func newScanner(path, fields []string, options Options) *jsoncapture.Scanner {
	return jsoncapture.NewScanner(jsoncapture.Options{
		ObjectPath: path,
		Fields:     fields,
		MaxBytes:   options.MaxResultBytes,
		MaxDepth:   options.MaxNestingDepth,
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
