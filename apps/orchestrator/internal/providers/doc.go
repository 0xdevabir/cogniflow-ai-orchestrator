// Package providers is the vendor-agnostic streaming layer.
//
// The Streamer interface accepts a Request and writes Chunks to a ChunkSink.
// Per-provider adapters (openai.go, anthropic.go, ollama.go, mistral.go,
// hf.go, mock.go) normalize the various wire formats into the same Chunk.
package providers
