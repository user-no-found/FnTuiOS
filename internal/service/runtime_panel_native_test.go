package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"fntuios/internal/gateway"
	"github.com/user-no-found/FnPackKit/runtime/backend"
	"github.com/user-no-found/FnPackKit/runtime/identity"
)

// nativeRuntimeTarget keeps the single-UID browser fixture faithful to the control protocol.
// Privileged launch and NSS isolation are separately tested in the container fixture.
type nativeRuntimeTarget struct {
	mu      sync.Mutex
	fixture *nativeFixture
}

func (n *nativeRuntimeTarget) Running(ctx context.Context, id identity.Request) (bool, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.fixture.worker != nil && n.fixture.worker.Alive(), nil
}
func (n *nativeRuntimeTarget) Start(ctx context.Context, id identity.Request) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.fixture.worker == nil || !n.fixture.worker.Alive() {
		n.fixture.startWorker()
	}
	return nil
}
func (n *nativeRuntimeTarget) Stop(ctx context.Context, id identity.Request) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.fixture.worker == nil {
		return nil
	}
	if err := n.fixture.worker.Terminate(40 * time.Second); err != nil {
		return err
	}
	n.fixture.worker = nil
	return nil
}

func serveNativeBroker(fixture *nativeFixture, connection net.Conn) {
	decoder, encoder := json.NewDecoder(connection), json.NewEncoder(connection)
	target := &nativeRuntimeTarget{fixture: fixture}
	controls := backend.New(target)
	for {
		var request gateway.BrokerRequest
		if decoder.Decode(&request) != nil {
			return
		}
		endpoint := gateway.Endpoint{UID: request.UID}
		user := fixture.users[0]
		var err error
		if request.UID != user.UID || request.Name != user.Name {
			err = errors.New("native fixture has one principal")
		} else if request.Control != nil {
			var state backend.Snapshot
			state, err = controls.Execute(context.Background(), request.Request, *request.Control)
			endpoint.Runtime = &state
		} else {
			err = controls.Use(context.Background(), request.Request, func() error {
				if err := target.Start(context.Background(), request.Request); err != nil {
					return err
				}
				endpoint = fixture.endpoint
				return nil
			})
		}
		if err != nil {
			endpoint.Error = "native fixture runtime unavailable"
			endpoint.Paused = errors.Is(err, backend.ErrPaused)
		}
		if encoder.Encode(endpoint) != nil {
			return
		}
	}
}
