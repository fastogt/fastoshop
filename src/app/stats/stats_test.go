package stats

import (
	"regexp"
	"testing"
	"time"

	"gitlab.com/fastogt/gofastogt/gofastogt"
)

// The values are machine-dependent, so we check not the numbers but that the
// collection doesn't panic and fills in the required fields: an empty
// memory_total on the page looks like the shop is broken, while it would be
// the collector that broke.
func TestGet(t *testing.T) {
	m := Get()

	if m.MemoryTotalBytes == 0 {
		t.Error("memory_total is zero")
	}
	if m.MemoryFreeBytes > m.MemoryTotalBytes {
		t.Errorf("memory_free %d > total %d", m.MemoryFreeBytes, m.MemoryTotalBytes)
	}
	if m.HddTotalBytes == 0 {
		t.Error("hdd_total is zero")
	}
	if m.HddFreeBytes > m.HddTotalBytes {
		t.Errorf("hdd_free %d > total %d", m.HddFreeBytes, m.HddTotalBytes)
	}
	if !regexp.MustCompile(`^\d+\.\d\d \d+\.\d\d \d+\.\d\d$`).MatchString(m.LoadAverage) {
		t.Errorf("load_average %q is not three numbers", m.LoadAverage)
	}
	if m.Uptime == 0 {
		t.Error("uptime is zero")
	}
	if m.OS.Arch == "" {
		t.Error("os.arch is empty")
	}

	now := gofastogt.NewUTCTimestamp()
	if age := time.Duration(now-m.UtcTimestamp) * time.Millisecond; age > time.Minute {
		t.Errorf("timestamp is %v old", age)
	}
}

// Speed is the delta between samples: the first call has nothing to compare
// against, and reporting the uptime average there would be a lie.
func TestNetworkSpeedNeedsTwoSamples(t *testing.T) {
	prevMu.Lock()
	prevIn, prevOut, prevTime = 0, 0, 0
	prevMu.Unlock()

	if _, _, in, out := Network(); in != 0 || out != 0 {
		t.Errorf("first sample must report zero speed, got in=%d out=%d", in, out)
	}
	totalIn, totalOut, _, _ := Network()
	if totalIn == 0 && totalOut == 0 {
		t.Skip("no traffic on this host")
	}
}
