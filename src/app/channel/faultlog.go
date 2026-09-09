package channel

import (
	"fmt"
	"sync"
)

// FaultLog keeps a worker's repeating complaint out of the journal. A sync pass
// runs on a timer, so a lasting fault - a token issued without a section, a
// platform that is down - writes the same line every few minutes for days and
// buries everything else in the log we read to see what a shop is doing.
type FaultLog struct {
	mu   sync.Mutex
	last string
	n    int
}

// Line reports what to log for this pass: the fault the first time it appears,
// nothing while it stands unchanged, and a closing note carrying the repeat
// count when it changes or clears. An empty msg means the pass succeeded.
func (f *FaultLog) Line(msg string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	if msg == f.last {
		if msg != "" {
			f.n++
		}
		return ""
	}
	closing := ""
	if f.last != "" {
		closing = fmt.Sprintf("%s - repeated %d more times, now %s", f.last, f.n,
			map[bool]string{true: "clear", false: "changed"}[msg == ""])
	}
	f.last, f.n = msg, 0
	switch {
	case closing == "":
		return msg
	case msg == "":
		return closing
	default:
		return closing + "; " + msg
	}
}
