package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/importer"
)

// Kinds and stages travel as keys: the admin renders them in the owner's language.
const (
	kJobImport = "import"
	kJobFill   = "fill"
)

const (
	kStagePhotos = "photos"
	kStageThumbs = "thumbs"
)

// A list, not one stage: averaging separate counters into one number would lie.
type jobStage struct {
	Task  string `json:"task"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
	// State is pending | running | done.
	State string `json:"state"`
}

const (
	kStagePending = "pending"
	kStageRunning = "running"
	kStageDone    = "done"
)

// job is the one long-running task an instance may have. A shop has one owner,
// who cannot start an import and a fill at the same moment, so a single slot
// with a mutex is the whole scheduler.
//
// ponytail: a restart loses the progress but no data - products are already
// written, photos that did not make it are still links, and running the action
// again picks up exactly what is left. A jobs table appears when there is more
// than one job.
type job struct {
	mu       sync.Mutex
	running  bool
	kind     string
	stages   []jobStage
	inFlight []int64
	result   *importer.Result
	err      string
	stopped  bool
	cancel   context.CancelFunc
}

// Returns ok=false when a job is already running.
func (j *job) start(kind string, stages []jobStage) (context.Context, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.running {
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	for i := range stages {
		stages[i].State = kStagePending
	}
	// Field by field, not *j = job{}: that would overwrite the mutex we hold.
	j.running, j.kind, j.stages = true, kind, stages
	j.inFlight = nil
	j.result, j.err, j.stopped, j.cancel = nil, "", false, cancel
	return ctx, true
}

func (j *job) busy() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running
}

// Work already in flight finishes; nothing new is started.
func (j *job) stop() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.running && j.cancel != nil {
		j.stopped = true
		j.cancel()
	}
}

// Everything before the current step is marked finished: steps run in order.
func (j *job) progress(task string, done, total int, inFlight []int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.inFlight = inFlight
	for i := range j.stages {
		switch {
		case j.stages[i].Task == task:
			j.stages[i].Done, j.stages[i].Total = done, total
			j.stages[i].State = kStageRunning
			return
		case j.stages[i].State != kStageDone:
			j.stages[i].State = kStageDone
		}
	}
}

func (j *job) finish(res *importer.Result, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancel != nil {
		j.cancel()
		j.cancel = nil
	}
	j.running, j.inFlight, j.result = false, nil, res
	// Only a run neither stopped nor broken fills its bars: 100% would be a lie.
	if err == nil && !j.stopped {
		for i := range j.stages {
			j.stages[i].State = kStageDone
			if j.stages[i].Total > 0 {
				j.stages[i].Done = j.stages[i].Total
			}
		}
	}
	if err != nil {
		j.err = err.Error()
	}
}

type jobResponse struct {
	Running bool       `json:"running"`
	Kind    string     `json:"kind"`
	Stages  []jobStage `json:"stages"`
	// InFlight are the products being worked on right now.
	InFlight []int64 `json:"in_flight"`
	// Stopped tells a half-finished result from a complete one.
	Stopped bool             `json:"stopped"`
	Result  *importer.Result `json:"result,omitempty"`
	Error   string           `json:"error,omitempty"`
}

// The snapshot for page load; live updates come from JobStream.
func (h *Handler) Job(w http.ResponseWriter, r *http.Request) {
	httpjson.WriteOK(w, h.jobState())
}

// state is what both the snapshot and the stream answer with.
func (h *Handler) jobState() jobResponse {
	h.job.mu.Lock()
	defer h.job.mu.Unlock()
	return jobResponse{
		Running: h.job.running, Kind: h.job.kind, Stages: h.job.stages,
		InFlight: h.job.inFlight, Stopped: h.job.stopped,
		Result: h.job.result, Error: h.job.err,
	}
}

// ponytail: the stream watches the job state on a ticker instead of being woken
// by it. A condition variable would save two wakeups a second and cost a
// subscriber list - worth it when there is more than one job to watch.
const kJobTick = 500 * time.Millisecond

// Keeps the connection alive through proxies that drop idle upstreams.
const kJobKeepAlive = 25 * time.Second

// Buffering is switched off explicitly: nginx would hold the events until the end.
func (h *Handler) JobStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	send := func(state jobResponse) string {
		data, err := json.Marshal(state)
		if err != nil {
			return ""
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return string(data)
	}
	last := send(h.jobState())

	tick := time.NewTicker(kJobTick)
	defer tick.Stop()
	keepAlive := time.NewTicker(kJobKeepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			_, _ = fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case <-tick.C:
			state := h.jobState()
			data, err := json.Marshal(state)
			if err != nil || string(data) == last {
				continue
			}
			last = send(state)
		}
	}
}

func (h *Handler) JobStop(w http.ResponseWriter, r *http.Request) {
	h.job.stop()
	httpjson.WriteOK(w, okStatusResponse{Status: "ok"})
}
