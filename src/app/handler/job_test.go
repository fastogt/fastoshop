package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/importer"
)

func jobState(t *testing.T, h *Handler) jobResponse {
	t.Helper()
	w := httptest.NewRecorder()
	h.Job(w, httptest.NewRequest("GET", "/api/job", nil))
	var resp struct {
		Data jobResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Data
}

// One owner, one long task: a second start must be refused.
func TestJobSlotIsSingle(t *testing.T) {
	h := newTestHandler(t)

	if idle := jobState(t, h); idle.Running {
		t.Fatal("fresh handler reports a running job")
	}
	if _, ok := h.job.start(kJobFill, []jobStage{{Task: kStagePhotos, Total: 10}}); !ok {
		t.Fatal("could not claim a free slot")
	}

	w := httptest.NewRecorder()
	h.BulkFill(w, httptest.NewRequest("POST", "/api/products/bulk/fill",
		strings.NewReader(`{"all":true}`)))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "задачи") {
		t.Fatalf("busy slot: %d %s", w.Code, w.Body.String())
	}

	h.job.progress(kStagePhotos, 4, 10, []int64{7})
	s := jobState(t, h)
	if !s.Running || s.Kind != kJobFill || len(s.Stages) != 1 ||
		s.Stages[0].Done != 4 || s.Stages[0].Total != 10 ||
		s.Stages[0].State != kStageRunning ||
		len(s.InFlight) != 1 || s.InFlight[0] != 7 {
		t.Fatalf("progress: %+v", s)
	}

	h.job.finish(&importer.Result{Imported: 9, Errors: 1}, nil)
	s = jobState(t, h)
	if s.Running || s.Result == nil || s.Result.Imported != 9 || s.InFlight != nil ||
		s.Stages[0].State != kStageDone || s.Stages[0].Done != 10 {
		t.Fatalf("finished: %+v", s)
	}
}

// Earlier steps show as finished, and a stopped run never claims a full bar.
func TestJobStagesAdvanceAndStop(t *testing.T) {
	h := newTestHandler(t)
	if _, ok := h.job.start(kJobImport, []jobStage{
		{Task: importer.StageFetch}, {Task: importer.StageProducts, Total: 100},
	}); !ok {
		t.Fatal("could not claim a free slot")
	}

	h.job.progress(importer.StageProducts, 30, 100, nil)
	s := jobState(t, h)
	if s.Stages[0].State != kStageDone || s.Stages[1].State != kStageRunning {
		t.Fatalf("stages: %+v", s.Stages)
	}

	h.job.stop()
	h.job.finish(&importer.Result{Imported: 30}, nil)
	s = jobState(t, h)
	if !s.Stopped || s.Stages[1].Done != 30 {
		t.Fatalf("a stopped run must not show a full bar: %+v", s)
	}
}

// Nothing to download is not an error and must not take the slot.
func TestBulkFillNothingToDo(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	h.BulkFill(w, httptest.NewRequest("POST", "/api/products/bulk/fill",
		strings.NewReader(`{"all":true}`)))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"started":true`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if jobState(t, h).Running {
		t.Fatal("slot taken with no work to do")
	}
}
