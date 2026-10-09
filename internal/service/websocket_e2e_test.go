package service

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/user-no-found/FnPackKit/runtime/identity"
)

type testWebSocket struct {
	connection net.Conn
	reader     *bufio.Reader
	mu         sync.Mutex
	ready      chan struct{}
}

func (fixture *containerFixture) connectWebSocket(user identity.User) *testWebSocket {
	t := fixture.t
	connection, err := net.Dial("unix", filepath.Join(fixture.cfg.AppDir, "app.sock"))
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest("GET", "http://nas.example/app/tuios/ws", nil)
	// Emulate the documented fnOS nginx Host $host transformation on a real handshake.
	request.Header.Set("Origin", "http://nas.example:5666")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", base64.StdEncoding.EncodeToString(key))
	request.Header.Set("X-Trim-Userid", strconv.FormatUint(uint64(user.UID), 10))
	request.Header.Set("X-Trim-Username", user.Name)
	if err := request.Write(connection); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 101 {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("WebSocket handshake: %d %s", response.StatusCode, body)
	}
	websocket := &testWebSocket{connection: connection, reader: reader, ready: make(chan struct{})}
	websocket.send(t, 2, []byte(`2{"cols":160,"rows":45}`))
	go func() {
		var ready sync.Once
		for {
			opcode, data, err := websocket.read()
			if err != nil {
				return
			}
			if opcode == 9 {
				websocket.write(10, data)
			}
			if len(data) > 0 && data[0] == '1' {
				ready.Do(func() { close(websocket.ready) })
			}
		}
	}()
	select {
	case <-websocket.ready:
	case <-time.After(15 * time.Second):
		t.Fatal("no terminal frames after WebSocket handshake")
	}
	t.Cleanup(func() { connection.Close() })
	return websocket
}

func (socket *testWebSocket) send(t *testing.T, opcode byte, data []byte) {
	t.Helper()
	if err := socket.write(opcode, data); err != nil {
		t.Fatal(err)
	}
}

func (socket *testWebSocket) write(opcode byte, data []byte) error {
	socket.mu.Lock()
	defer socket.mu.Unlock()
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header := []byte{0x80 | opcode}
	if len(data) < 126 {
		header = append(header, byte(len(data))|0x80)
	} else if len(data) < 65536 {
		header = append(header, 126|0x80, byte(len(data)>>8), byte(len(data)))
	} else {
		return fmt.Errorf("test frame exceeds 64 KiB")
	}
	header = append(header, mask...)
	frame := append([]byte{}, data...)
	for index := range frame {
		frame[index] ^= mask[index%4]
	}
	_, err := socket.connection.Write(append(header, frame...))
	return err
}

func (socket *testWebSocket) read() (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(socket.reader, header); err != nil {
		return 0, nil, err
	}
	length := uint64(header[1] & 127)
	if length == 126 {
		data := make([]byte, 2)
		if _, err := io.ReadFull(socket.reader, data); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(data))
	}
	if length == 127 {
		data := make([]byte, 8)
		if _, err := io.ReadFull(socket.reader, data); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(data)
	}
	if length > 8<<20 {
		return 0, nil, fmt.Errorf("oversized server frame")
	}
	if header[1]&128 != 0 {
		return 0, nil, fmt.Errorf("unexpected masked server frame")
	}
	data := make([]byte, int(length))
	_, err := io.ReadFull(socket.reader, data)
	return header[0] & 15, data, err
}
