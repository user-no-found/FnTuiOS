package imageupload

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func upload(t *testing.T, s *Store, token string, body []byte) int {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/image-paste/"+token, bytes.NewReader(body))
	w := &deadlineRecorder{httptest.NewRecorder()}
	s.ServeHTTP(w, r)
	return w.Code
}

func newTestStore() *Store {
	return New(func(b []byte) bool { return bytes.HasPrefix(b, []byte("PNG")) })
}

type deadlineRecorder struct{ *httptest.ResponseRecorder }

func (*deadlineRecorder) SetReadDeadline(time.Time) error { return nil }

func TestCapabilityBoundary(t *testing.T) {
	for _, mode := range []string{"replay", "invalid", "expired", "disconnect", "other-user"} {
		t.Run(mode, func(t *testing.T) {
			s := newTestStore()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var delivered atomic.Int32
			token, err := s.Issue(ctx, "connection", func(b []byte, done func(string)) { delivered.Add(1); done("") }, func(string) {})
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "replay":
				if got := upload(t, s, token, []byte("PNGdata")); got != 202 {
					t.Fatalf("first: %d", got)
				}
			case "invalid":
				if got := upload(t, s, token, []byte("shell")); got != 415 {
					t.Fatalf("invalid: %d", got)
				}
			case "expired":
				s.mu.Lock()
				s.tokens[token].expires = time.Now().Add(-time.Second)
				s.mu.Unlock()
			case "disconnect":
				cancel()
			case "other-user":
				if got := upload(t, newTestStore(), token, []byte("PNGdata")); got != 410 {
					t.Fatalf("foreign store: %d", got)
				}
				return
			}
			if got := upload(t, s, token, []byte("PNGdata")); got != 410 {
				t.Fatalf("reuse/revoked: %d", got)
			}
			if mode != "replay" && delivered.Load() != 0 {
				t.Fatal("revoked token delivered data")
			}
		})
	}
}

func TestBoundedUploadsAndSingleConnection(t *testing.T) {
	s := newTestStore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	issue := func(owner string) string {
		t.Helper()
		token, err := s.Issue(ctx, owner, func(b []byte, done func(string)) { done("") }, func(string) {})
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	token := issue("one")
	if _, err := s.Issue(ctx, "one", nil, nil); err == nil {
		t.Fatal("same connection acquired another token")
	}
	tooBig := append([]byte("PNG"), make([]byte, MaxBytes)...)
	if got := upload(t, s, token, tooBig); got != 413 {
		t.Fatalf("oversize: %d", got)
	}
	if got := upload(t, s, token, []byte("PNG")); got != 410 {
		t.Fatalf("failed token replay: %d", got)
	}
	token = issue("one")
	r := httptest.NewRequest(http.MethodPost, "/image-paste/"+token, strings.NewReader(string(tooBig)))
	r.ContentLength = -1
	w := &deadlineRecorder{httptest.NewRecorder()}
	s.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatalf("chunked oversize: %d", w.Code)
	}
	if got := upload(t, s, strings.Repeat("x", 4096), []byte("PNG")); got != 410 {
		t.Fatalf("oversize token: %d", got)
	}
}

func TestConcurrentClaimOnlyDeliversOnce(t *testing.T) {
	s := newTestStore()
	var delivered atomic.Int32
	completed := make(chan struct{}, 1)
	token, err := s.Issue(context.Background(), "one", func(b []byte, done func(string)) {
		delivered.Add(1)
		done("")
		completed <- struct{}{}
	}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	codes := make(chan int, 2)
	for range 2 {
		go func() { codes <- upload(t, s, token, []byte("PNGdata")) }()
	}
	a, b := <-codes, <-codes
	if a+b != 202+410 {
		t.Fatalf("claims: %d %d", a, b)
	}
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("no delivery")
	}
	if delivered.Load() != 1 {
		t.Fatal("duplicate delivery")
	}
}

func TestHTTPRequiresEnforceableBodyDeadline(t *testing.T) {
	s := newTestStore()
	token, err := s.Issue(context.Background(), "one", func([]byte, func(string)) {
		t.Error("upload delivered without an enforceable deadline")
	}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/image-paste/"+token, strings.NewReader("PNGdata"))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unsupported deadline accepted: %d", w.Code)
	}
}
