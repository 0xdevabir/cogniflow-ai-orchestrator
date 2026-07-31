package citation

import (
	"sync/atomic"
	"time"
)

// nowNano is a package-level seam so tests can override time without injecting
// a clock interface everywhere. It returns the current unix-nano.
var nowNano = func() int64 { return time.Now().UnixNano() }

// spanCounter ensures two callers in the same nanosecond still get distinct IDs.
var spanCounterUint uint64

func nextSpanCounter() uint64 {
	return atomic.AddUint64(&spanCounterUint, 1)
}
