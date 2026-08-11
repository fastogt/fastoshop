package stats

import (
	"regexp"
	"testing"
	"time"

	"gitlab.com/fastogt/gofastogt/gofastogt"
)

// Значения машинозависимы, поэтому проверяем не числа, а что сбор не паникует и
// заполняет обязательное: пустой memory_total на странице выглядит как поломка
// магазина, хотя сломан был бы сборщик.
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

// Скорость — дельта между замерами: первый вызов сравнивать не с чем, и
// выдавать там среднюю за аптайм было бы враньём.
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
