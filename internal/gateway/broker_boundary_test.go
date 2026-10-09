package gateway

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/user-no-found/FnPackKit/runtime/identity"
)

// A late response must never become another NAS user's endpoint after a timeout.
func TestBrokerTimeoutCannotCrossIdentity(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer clientSide.Close()
	defer serverSide.Close()
	client := &brokerClient{connection: clientSide, decoder: json.NewDecoder(clientSide), encoder: json.NewEncoder(clientSide), timeout: 20 * time.Millisecond}
	late := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		var request identity.Request
		if json.NewDecoder(serverSide).Decode(&request) != nil {
			return
		}
		<-late
		// Permit a new request to enter the full-duplex stream while the old
		// response is delayed, as a real buffered Unix socket does.
		go func() { var next identity.Request; json.NewDecoder(serverSide).Decode(&next) }()
		json.NewEncoder(serverSide).Encode(Endpoint{UID: 1000, Address: "127.0.0.1:12345", Password: strings.Repeat("a", 43)})
	}()
	if _, err := client.endpoint(identity.Request{UID: 1000, Name: "alice"}); err == nil {
		t.Fatal("first request did not time out")
	}
	close(late)
	if endpoint, err := client.endpoint(identity.Request{UID: 1001, Name: "bob"}); err == nil {
		t.Fatalf("late user A response routed to user B: %+v", endpoint)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("failed control channel was left open")
	}
}

func TestBrokerRejectsUncorrelatedIdentity(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer clientSide.Close()
	defer serverSide.Close()
	client := &brokerClient{connection: clientSide, decoder: json.NewDecoder(clientSide), encoder: json.NewEncoder(clientSide), timeout: time.Second}
	go func() {
		var request identity.Request
		json.NewDecoder(serverSide).Decode(&request)
		json.NewEncoder(serverSide).Encode(Endpoint{UID: 1001, Address: "127.0.0.1:12345", Password: strings.Repeat("b", 43)})
	}()
	if _, err := client.endpoint(identity.Request{UID: 1000, Name: "alice"}); err == nil {
		t.Fatal("accepted endpoint for another UID")
	}
}
