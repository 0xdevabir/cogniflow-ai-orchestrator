package rag

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

// ChunkOpts controls text splitting. Zero values use the production defaults.
type ChunkOpts struct {
	MaxChars      int // target maximum window size; default 800
	OverlapChars  int // overlap with previous window; default 200
	MinChunkChars int // fragments shorter than this are folded/dropped; default 100
}

func (o ChunkOpts) normalized() ChunkOpts {
	if o.MaxChars <= 0 {
		o.MaxChars = 800
	}
	if o.OverlapChars < 0 {
		o.OverlapChars = 0
	}
	if o.OverlapChars == 0 {
		o.OverlapChars = 200
	}
	if o.OverlapChars >= o.MaxChars {
		o.OverlapChars = o.MaxChars / 4
	}
	if o.MinChunkChars <= 0 {
		o.MinChunkChars = 100
	}
	return o
}

// SplitText splits text into sentence-aware, overlapping windows. CharStart
// and CharEnd are offsets into the original UTF-8 string (byte offsets, which
// are also valid slice indexes and work with Postgres substring provenance).
//
// It prefers sentence/newline/whitespace boundaries within the final 30% of
// each window. If none exists, it hard-cuts at MaxChars. The next chunk starts
// approximately OverlapChars before the previous end, moved to the next word
// boundary so chunks never begin in the middle of a word.
func SplitText(text string, opts ChunkOpts) []Chunk {
	opts = opts.normalized()
	text = strings.TrimSpace(text)
	if len(text) < opts.MinChunkChars {
		return nil
	}

	out := make([]Chunk, 0, len(text)/(opts.MaxChars-opts.OverlapChars)+1)
	start := 0
	for start < len(text) {
		end := start + opts.MaxChars
		if end >= len(text) {
			end = len(text)
		} else {
			end = bestBoundary(text, start, end)
		}
		if end <= start {
			end = min(start+opts.MaxChars, len(text))
		}

		chunkStart, chunkEnd := trimBounds(text, start, end)
		if chunkEnd-chunkStart >= opts.MinChunkChars || len(out) == 0 {
			out = append(out, Chunk{
				ID:        chunkID(text[chunkStart:chunkEnd], chunkStart),
				Text:      text[chunkStart:chunkEnd],
				CharStart: chunkStart,
				CharEnd:   chunkEnd,
			})
		} else if len(out) > 0 {
			// Preserve a tiny final fragment by folding it into the prior chunk.
			last := &out[len(out)-1]
			last.Text = text[last.CharStart:chunkEnd]
			last.CharEnd = chunkEnd
		}

		if end >= len(text) {
			break
		}
		next := end - opts.OverlapChars
		if next <= start {
			next = end
		}
		// Move forward to a word boundary, but never consume more than 25% of
		// the requested overlap.
		limit := min(next+opts.OverlapChars/4, end)
		for next < limit && next < len(text) && !unicode.IsSpace(rune(text[next])) {
			next++
		}
		for next < len(text) && unicode.IsSpace(rune(text[next])) {
			next++
		}
		start = next
	}
	return out
}

func bestBoundary(text string, start, maxEnd int) int {
	floor := start + (maxEnd-start)*7/10
	for i := maxEnd - 1; i >= floor; i-- {
		switch text[i] {
		case '\n':
			return i + 1
		case '.', '!', '?':
			if i+1 >= len(text) || unicode.IsSpace(rune(text[i+1])) {
				return i + 1
			}
		}
	}
	for i := maxEnd - 1; i >= floor; i-- {
		if unicode.IsSpace(rune(text[i])) {
			return i
		}
	}
	return maxEnd
}

func trimBounds(text string, start, end int) (int, int) {
	for start < end && unicode.IsSpace(rune(text[start])) {
		start++
	}
	for end > start && unicode.IsSpace(rune(text[end-1])) {
		end--
	}
	return start, end
}

func chunkID(text string, start int) string {
	h := sha256.Sum256([]byte(text + "\x00" + itoa(start)))
	return "chk_" + hex.EncodeToString(h[:8])
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
