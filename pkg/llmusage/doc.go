// Package llmusage incrementally extracts and normalizes token usage from LLM
// API response bodies.
//
// A Decoder consumes exactly one response. Call Feed with the bytes read from
// the body, then call Finish at EOF or close. The decoder never modifies input
// bytes and does not perform HTTP requests, logging, pricing, or delivery.
//
// Protocol names describe wire contracts, not the company serving a request.
// For example, an OpenAI-compatible gateway may use an OpenAI protocol while
// having a different billing provider.
package llmusage
