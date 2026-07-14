package llmusage_test

import (
	"fmt"
	"log"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage"
)

func ExampleParse() {
	body := []byte(`{
  "id":"resp_123",
  "model":"gpt-5.4",
  "usage":{"input_tokens":12,"output_tokens":4,"total_tokens":16}
}`)
	results, err := llmusage.Parse(body, llmusage.Options{
		Protocol: llmusage.ProtocolOpenAIResponses,
		Format:   llmusage.FormatJSON,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s: %d tokens\n", results[0].Model, results[0].Usage.TotalTokens)
	// Output: gpt-5.4: 16 tokens
}

func ExampleDecoder() {
	decoder, err := llmusage.NewDecoder(llmusage.Options{
		Protocol: llmusage.ProtocolOpenAIResponses,
		Format:   llmusage.FormatSSE,
	})
	if err != nil {
		log.Fatal(err)
	}

	chunks := [][]byte{
		[]byte("event: response.completed\n"),
		[]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_stream\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n"),
	}
	for _, chunk := range chunks {
		results, feedErr := decoder.Feed(chunk)
		if feedErr != nil {
			log.Fatal(feedErr)
		}
		for _, result := range results {
			fmt.Printf("%s: %d tokens\n", result.ResponseID, result.Usage.TotalTokens)
		}
	}
	if _, err = decoder.Finish(); err != nil {
		log.Fatal(err)
	}
	// Output: resp_stream: 5 tokens
}
