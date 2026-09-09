package channel

import (
	"strings"
	"testing"
)

// A worker retries every few minutes, so one lasting fault used to write the
// same line for days: on a live shop a token without the Marketplace section
// filled two days of journal at five-minute intervals and buried everything
// else, including the photo downloads and the rate limits worth seeing.
func TestFaultLogSpeaksOnceAndSaysWhenItEnds(t *testing.T) {
	var f FaultLog
	const scope = "wb sync: 403 scope is not allowed"

	if got := f.Line(scope); got != scope {
		t.Errorf("first fault must be logged as it stands, got %q", got)
	}
	for i := range 500 {
		if got := f.Line(scope); got != "" {
			t.Fatalf("repeat %d spoke again: %q", i, got)
		}
	}

	got := f.Line("")
	if !strings.Contains(got, "500") {
		t.Errorf("the closing line does not carry the repeat count: %q", got)
	}
	if !strings.Contains(got, "clear") {
		t.Errorf("the closing line does not say the fault ended: %q", got)
	}
	// Cleared and quiet: a healthy worker writes nothing at all.
	if got := f.Line(""); got != "" {
		t.Errorf("a passing worker spoke: %q", got)
	}
}

// A different fault is news even while the previous one was being suppressed.
func TestFaultLogReportsAChangedFault(t *testing.T) {
	var f FaultLog
	f.Line("wb sync: 403")
	f.Line("wb sync: 403")

	got := f.Line("wb sync: 429")
	if !strings.Contains(got, "403") || !strings.Contains(got, "429") {
		t.Errorf("a changed fault must close the old one and open the new: %q", got)
	}
	if !strings.Contains(got, "changed") {
		t.Errorf("the closing note does not say the fault changed: %q", got)
	}
	if got := f.Line("wb sync: 429"); got != "" {
		t.Errorf("the new fault repeated itself: %q", got)
	}
}
