package service

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/user-no-found/FnPackKit/runtime/backend"
	"github.com/user-no-found/FnPackKit/runtime/identity"
)

func (b *broker) controller() *backend.Controller {
	b.controlOnce.Do(func() { b.controls = backend.New(brokerTarget{b}) })
	return b.controls
}

type brokerTarget struct{ broker *broker }

func (t brokerTarget) resolve(request identity.Request) (identity.User, error) {
	if request.UID == t.broker.gatewayUID {
		return identity.User{}, errors.New("service identity cannot control terminals")
	}
	return identity.Resolve(request)
}

func (t brokerTarget) Running(ctx context.Context, request identity.Request) (bool, error) {
	user, err := t.resolve(request)
	if err != nil {
		return false, err
	}
	t.broker.mu.Lock()
	defer t.broker.mu.Unlock()
	current := t.broker.instances[user.UID]
	if current == nil {
		return false, nil
	}
	if !reflect.DeepEqual(current.user, user) {
		return false, errors.New("runtime identity changed")
	}
	return current.worker.Alive(), nil
}

func (t brokerTarget) Start(ctx context.Context, request identity.Request) error {
	_, err := t.broker.acquire(request)
	return err
}

func (t brokerTarget) Stop(ctx context.Context, request identity.Request) error {
	user, err := t.resolve(request)
	if err != nil {
		return err
	}
	t.broker.mu.Lock()
	defer t.broker.mu.Unlock()
	current := t.broker.instances[user.UID]
	if current == nil {
		return nil
	}
	if !reflect.DeepEqual(current.user, user) {
		return errors.New("runtime identity changed")
	}
	// The worker saves the daemon before ending its web process. No forced kill bypasses that save.
	if err := current.worker.Terminate(time.Duration(t.broker.config.ShutdownTimeoutSeconds+5) * time.Second); err != nil {
		return err
	}
	delete(t.broker.instances, user.UID)
	return nil
}
