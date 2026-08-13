package handler

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/process"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/stats"
	"github.com/fastogt/fastoshop/app/version"
)

type shopStats struct {
	Products        int     `json:"products"`
	ProductsVisible int     `json:"products_visible"`
	Orders          int     `json:"orders"`
	OrdersNew       int     `json:"orders_new"`
	DatabaseBytes   int64   `json:"database_bytes"`
	UploadsBytes    int64   `json:"uploads_bytes"`
	UploadsFiles    int     `json:"uploads_files"`
	ProcessRSSBytes uint64  `json:"process_rss_bytes"`
	ProcessCPU      float64 `json:"process_cpu"`
	ProcessUptime   int64   `json:"process_uptime"`
	Version         string  `json:"version"`
}

type statsResponse struct {
	Server stats.Machine `json:"server"`
	Shop   shopStats     `json:"shop"`
}

// Walking uploads means tens of thousands of files on a 24,000-product catalog.
// An open admin tab must not run du in a loop, so the size lives for a minute:
// it doesn't change enough within a minute for anyone to notice.
const kUploadsSizeTTL = time.Minute

var (
	uploadsMu    sync.Mutex
	uploadsAt    time.Time
	uploadsBytes int64
	uploadsFiles int
)

func dirSize(dir string) (int64, int) {
	uploadsMu.Lock()
	defer uploadsMu.Unlock()
	if time.Since(uploadsAt) < kUploadsSizeTTL {
		return uploadsBytes, uploadsFiles
	}
	var total int64
	var count int
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
			count++
		}
		return nil
	})
	uploadsBytes, uploadsFiles, uploadsAt = total, count, time.Now()
	return total, count
}

// dbSize counts the -wal too: under WAL part of the data lives in it, and the
// size of the .db alone understates the picture the more active the shop is.
func dbSize(path string) int64 {
	var total int64
	for _, p := range []string{path, path + "-wal"} {
		if fi, err := os.Stat(p); err == nil {
			total += fi.Size()
		}
	}
	return total
}

func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	products, _ := h.db.CountProducts("", database.AnySupplier)
	visible, _ := h.db.CountVisibleProducts("", "")
	orders, _ := h.db.CountOrders("")
	ordersNew, _ := h.db.CountOrders("new")

	uploadsSize, uploadsCount := dirSize(h.uploadsDir)
	shop := shopStats{
		Products:        products,
		ProductsVisible: visible,
		Orders:          orders,
		OrdersNew:       ordersNew,
		DatabaseBytes:   dbSize(h.db.Path()),
		UploadsBytes:    uploadsSize,
		UploadsFiles:    uploadsCount,
		Version:         version.VersionApp,
	}

	// Ask the system about the process: RSS from runtime.MemStats shows the Go
	// heap, while the owner needs the figure systemd uses for MemoryMax.
	if p, err := process.NewProcess(int32(os.Getpid())); err == nil {
		if m, err := p.MemoryInfo(); err == nil {
			shop.ProcessRSSBytes = m.RSS
		}
		shop.ProcessCPU, _ = p.CPUPercent()
		if started, err := p.CreateTime(); err == nil {
			shop.ProcessUptime = (time.Now().UnixMilli() - started) / 1000
		}
	}

	writeOK(w, statsResponse{Server: stats.Get(), Shop: shop})
}
