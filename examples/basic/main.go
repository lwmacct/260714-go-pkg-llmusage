package main

import (
	"fmt"
	"log"

	"github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage"
)

func main() {
	decoder, err := llmusage.NewDecoder(llmusage.Options{
		Protocol: llmusage.ProtocolOpenAIResponses,
		Format:   llmusage.FormatSSE,
	})
	if err != nil {
		log.Fatal(err)
	}

	stream := []byte("event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_example\",\"model\":\"gpt-5.4\",\"usage\":{\"input_tokens\":8,\"output_tokens\":5,\"total_tokens\":13}}}\n\n")
	results, err := decoder.Feed(stream)
	if err != nil {
		log.Fatal(err)
	}
	final, err := decoder.Finish()
	if err != nil {
		log.Fatal(err)
	}
	results = append(results, final...)
	for _, result := range results {
		fmt.Printf("response=%s model=%s total=%d\n", result.ResponseID, result.Model, result.Usage.TotalTokens)
	}
}
