package imageupload

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// ServeHTTP accepts a raw image body under the web server's authenticated route.
func (s *Store) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "invalid_request")
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/image-paste/")
	if len(token) != 64 || strings.ContainsAny(token, "/%") {
		fail(w, http.StatusGone, "expired")
		return
	}
	t, code := s.claim(token)
	if code != "" {
		status := http.StatusGone
		if code == "busy" {
			status = http.StatusTooManyRequests
		}
		fail(w, status, code)
		return
	}
	failure := func(status int, code string) { s.finish(t, code); fail(w, status, code) }
	if r.ContentLength > MaxBytes {
		failure(http.StatusRequestEntityTooLarge, "too_large")
		return
	}
	deadline := http.NewResponseController(w)
	if err := deadline.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
		failure(http.StatusBadRequest, "upload_failed")
		return
	}
	defer func() { _ = deadline.SetReadDeadline(time.Time{}) }()
	body := http.MaxBytesReader(w, r.Body, MaxBytes)
	defer body.Close()
	stop := context.AfterFunc(t.ctx, func() { _ = body.Close() })
	defer stop()
	data, err := io.ReadAll(body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			failure(http.StatusRequestEntityTooLarge, "too_large")
		} else {
			failure(http.StatusBadRequest, "upload_failed")
		}
		return
	}
	if t.ctx.Err() != nil {
		failure(http.StatusGone, "expired")
		return
	}
	if s.valid == nil || !s.valid(data) {
		failure(http.StatusUnsupportedMediaType, "invalid_image")
		return
	}
	// The daemon may send the image over a remote pane's link. Its completion
	// is reported on the original WS, outside the proxy's HTTP header timeout.
	w.WriteHeader(http.StatusAccepted)
	_, _ = io.WriteString(w, "{\"status\":\"accepted\"}\n")
	go t.deliver(data, func(code string) { s.finish(t, code) })
}

func fail(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
