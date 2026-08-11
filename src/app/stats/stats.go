// Package stats собирает метрики машины в том же формате, что отдаёт
// gofastocloud_backend: одинаковые поля читаются одинаково во всех продуктах.
package stats

import (
	"fmt"
	"sync"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"gitlab.com/fastogt/gofastogt/gofastogt"
)

// Machine — снимок машины плюс то, что не меняется между замерами.
type Machine struct {
	gofastogt.Machine
	OS      gofastogt.OperationSystem `json:"os"`
	VSystem string                    `json:"vsystem"`
	VRole   string                    `json:"vrole"`
}

var (
	hostOnce sync.Once
	hostInfo *host.InfoStat
	osOnce   sync.Once
	osInfo   gofastogt.OperationSystem
)

// host.Info() читает файлы в /sys и /etc на каждый вызов, а между запросами
// эти данные не меняются — считаем один раз за жизнь процесса.
func sysHost() *host.InfoStat {
	hostOnce.Do(func() { hostInfo, _ = host.Info() })
	return hostInfo
}

func operationSystem() gofastogt.OperationSystem {
	osOnce.Do(func() {
		if h := sysHost(); h != nil {
			osInfo.Name = gofastogt.NewPlatformTypeFromString(h.OS)
			osInfo.Arch = h.KernelArch
			osInfo.Version = h.KernelVersion
		}
		osInfo.RamTotal, osInfo.RamFree = Memory()
	})
	return osInfo
}

// Скорость сети — дельта к прошлому замеру, поэтому предыдущий снимок живёт в
// пакете: страницу открывают из одной админки, передавать его через вызывающего
// (как в gofastocloud_backend) здесь было бы лишним звеном.
var (
	prevMu   sync.Mutex
	prevIn   uint64
	prevOut  uint64
	prevTime gofastogt.UtcTimeMsec
)

func Get() Machine {
	memTotal, memFree := Memory()
	hddTotal, hddFree := Hdd()
	totalIn, totalOut, speedIn, speedOut := Network()

	// ponytail: cost всегда 0 — тарификации по ресурсам у магазина нет.
	mach := gofastogt.NewMachine(CPUPercent(), 0, LoadAverage(),
		memTotal, memFree, hddTotal, hddFree,
		speedIn, speedOut, gofastogt.NewUTCTimestamp(), Uptime(),
		totalIn, totalOut, 0)

	m := Machine{Machine: mach, OS: operationSystem()}
	if h := sysHost(); h != nil {
		m.VSystem = h.VirtualizationSystem
		m.VRole = h.VirtualizationRole
	}
	return m
}

func CPUPercent() float64 {
	percents, err := cpu.Percent(0, false)
	if err != nil || len(percents) == 0 {
		return 0
	}
	return percents[0]
}

func LoadAverage() string {
	l, err := load.Avg()
	if err != nil {
		return "0 0 0"
	}
	return fmt.Sprintf("%.2f %.2f %.2f", l.Load1, l.Load5, l.Load15)
}

func Memory() (total, free uint64) {
	m, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0
	}
	return m.Total, m.Available
}

func Hdd() (total, free uint64) {
	d, err := disk.Usage("/")
	if err != nil {
		return 0, 0
	}
	return d.Total, d.Free
}

func Uptime() gofastogt.DurationSec {
	up, err := host.Uptime()
	if err != nil {
		return 0
	}
	return gofastogt.DurationSec(up)
}

// Network возвращает суммарные байты и скорость с прошлого замера. Первый
// вызов после старта отдаёт нулевую скорость: сравнивать ещё не с чем, а
// делить накопленные байты на аптайм — это средняя за всё время, а не текущая.
func Network() (totalIn, totalOut, speedIn, speedOut uint64) {
	counters, err := net.IOCounters(true)
	if err != nil {
		return 0, 0, 0, 0
	}
	for _, c := range counters {
		if c.Name == "lo" {
			continue
		}
		totalIn += c.BytesRecv
		totalOut += c.BytesSent
	}

	now := gofastogt.NewUTCTimestamp()
	prevMu.Lock()
	defer prevMu.Unlock()
	if prevTime != 0 && now > prevTime {
		sec := uint64((now - prevTime) / 1000)
		if sec == 0 {
			sec = 1
		}
		if totalIn >= prevIn {
			speedIn = (totalIn - prevIn) / sec
		}
		if totalOut >= prevOut {
			speedOut = (totalOut - prevOut) / sec
		}
	}
	prevIn, prevOut, prevTime = totalIn, totalOut, now
	return totalIn, totalOut, speedIn, speedOut
}
