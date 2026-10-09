// Package imageupload provides short-lived browser upload capabilities.
// It stores no files; the attached model owns validation and final delivery.
package imageupload

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// MaxBytes is the existing daemon's maximum image payload size.
const MaxBytes = 8 << 20

// Lifetime is how long a browser may wait before claiming a token.
const Lifetime = 30 * time.Second

// Delivery queues bytes on the captured model and completes after path paste.
type Delivery func([]byte, func(string))

type ticket struct {
	ctx      context.Context
	owner    string
	token    string
	expires  time.Time
	deliver  Delivery
	notify   func(string)
	stop     func() bool
	timer    *time.Timer
	active   bool
	finished bool
	notified bool
}

// Store belongs to one user's web process. Capabilities never cross stores.
type Store struct {
	mu     sync.Mutex
	tokens map[string]*ticket
	owners map[string]*ticket
	slots  chan struct{}
	valid  func([]byte) bool
}

// New constructs a store using the daemon's authoritative format recognizer.
func New(valid func([]byte) bool) *Store {
	return &Store{tokens: make(map[string]*ticket), owners: make(map[string]*ticket), slots: make(chan struct{}, 2), valid: valid}
}

// Issue binds one token to one live browser connection and captured delivery.
func (s *Store) Issue(ctx context.Context, owner string, deliver Delivery, notify func(string)) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if s.owners[owner] != nil || len(s.owners) >= 32 {
		return "", errors.New("busy")
	}
	t := &ticket{ctx: ctx, owner: owner, token: token, expires: time.Now().Add(Lifetime), deliver: deliver, notify: notify}
	s.tokens[token], s.owners[owner] = t, t
	t.stop = context.AfterFunc(ctx, func() { s.revoke(t) })
	t.timer = time.AfterFunc(Lifetime, func() {
		s.mu.Lock()
		pending := s.tokens[token] == t
		s.mu.Unlock()
		if pending {
			s.finish(t, "expired")
		}
	})
	return token, nil
}

// claim consumes before body validation, including requests that later fail.
func (s *Store) claim(token string) (*ticket, string) {
	s.mu.Lock()
	t := s.tokens[token]
	if t == nil {
		s.mu.Unlock()
		return nil, "expired"
	}
	delete(s.tokens, token)
	t.timer.Stop()
	code := ""
	if t.ctx.Err() != nil || !time.Now().Before(t.expires) {
		code = "expired"
	} else {
		select {
		case s.slots <- struct{}{}:
			t.active = true
		default:
			code = "busy"
		}
	}
	s.mu.Unlock()
	if code != "" {
		s.finish(t, code)
		return nil, code
	}
	return t, ""
}

func (s *Store) finish(t *ticket, code string) {
	s.mu.Lock()
	if t.finished {
		s.mu.Unlock()
		return
	}
	t.finished = true
	delete(s.tokens, t.token)
	delete(s.owners, t.owner)
	t.timer.Stop()
	t.stop()
	if t.active {
		<-s.slots
	}
	notify := !t.notified
	t.notified = true
	s.mu.Unlock()
	if notify && t.notify != nil {
		t.notify(code)
	}
}

// A canceled daemon call still occupies memory and a transfer slot until its
// actual completion. Revocation only prevents final model delivery meanwhile.
func (s *Store) revoke(t *ticket) {
	s.mu.Lock()
	if !t.active {
		s.mu.Unlock()
		s.finish(t, "expired")
		return
	}
	notify := !t.notified && !t.finished
	t.notified = true
	s.mu.Unlock()
	if notify && t.notify != nil {
		t.notify("expired")
	}
}
