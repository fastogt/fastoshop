package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

func uploadLogo(t *testing.T, h *Handler, name string, data []byte) settingsResponse {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", name)
	_, _ = part.Write(data)
	_ = mw.Close()
	req := httptest.NewRequest("POST", "/api/settings/logo", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.UploadLogo(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("upload logo: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data settingsResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Data
}

func TestLogoUploadReplaceDelete(t *testing.T) {
	h := newTestHandler(t)
	if err := h.db.CreateSettings(&database.Settings{OwnerEmail: "a@b.c"}); err != nil {
		t.Fatal(err)
	}

	first := uploadLogo(t, h, "logo.png", []byte("\x89PNG\r\n\x1a\npixels"))
	if first.Logo == "" {
		t.Fatal("ответ на загрузку не содержит имя файла")
	}
	if _, err := os.Stat(filepath.Join(h.uploadsDir, first.Logo)); err != nil {
		t.Fatalf("файл не сохранён: %v", err)
	}

	second := uploadLogo(t, h, "other.png", []byte("\x89PNG\r\n\x1a\nother"))
	if second.Logo == first.Logo {
		t.Fatal("замена логотипа не поменяла имя файла")
	}
	if _, err := os.Stat(filepath.Join(h.uploadsDir, first.Logo)); !os.IsNotExist(err) {
		t.Fatal("старый файл не удалён при замене")
	}

	w := httptest.NewRecorder()
	h.DeleteLogo(w, httptest.NewRequest("DELETE", "/api/settings/logo", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("delete logo: %d %s", w.Code, w.Body.String())
	}
	s, err := h.db.GetSettings()
	if err != nil || s.Logo != "" {
		t.Fatalf("логотип не сброшен: %+v %v", s, err)
	}
	if _, err := os.Stat(filepath.Join(h.uploadsDir, second.Logo)); !os.IsNotExist(err) {
		t.Fatal("файл не удалён")
	}
}
