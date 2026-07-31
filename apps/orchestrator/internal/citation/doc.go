// Package citation models the provenance graph for every claim.
//
// Every Chunk carries SpanRef slices. The CitationManifest is versioned and
// threaded through the DAG. At the end, an SSE manifest event ships the
// full graph to the web app for inline rendering.
package citation
