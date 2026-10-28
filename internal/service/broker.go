package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"time"

	"fntuios/internal/config"
	"fntuios/internal/gateway"
	"github.com/user-no-found/FnPackKit/runtime/backend"
	"github.com/user-no-found/FnPackKit/runtime/identity"
	"github.com/user-no-found/FnPackKit/runtime/process"
	"github.com/user-no-found/FnPackKit/runtime/securefs"
)

type instance struct {
	user     identity.User
	worker   *child
	endpoint gateway.Endpoint
}

type broker struct {
	config      config.Config
	self        string
	gatewayUID  uint32
	mu          sync.Mutex
	instances   map[uint32]*instance
	stopping    bool
	controlOnce sync.Once
	controls    *backend.Controller
}

func (broker *broker) serve(connection net.Conn) error {
	scanner := bufio.NewScanner(connection)
	scanner.Buffer(make([]byte, 4096), 4096)
	encoder := json.NewEncoder(connection)
	for scanner.Scan() {
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		var request gateway.BrokerRequest
		err := decoder.Decode(&request)
		var trailing any
		if err == nil && decoder.Decode(&trailing) != io.EOF {
			err = errors.New("trailing broker input")
		}
		var endpoint gateway.Endpoint
		if err == nil {
			if request.Control != nil {
				var snapshot backend.Snapshot
				snapshot, err = broker.controller().Execute(context.Background(), request.Request, *request.Control)
				endpoint = gateway.Endpoint{UID: request.UID, Runtime: &snapshot}
			} else {
				err = broker.controller().Use(context.Background(), request.Request, func() error {
					var acquireErr error
					endpoint, acquireErr = broker.acquire(request.Request)
					return acquireErr
				})
			}
		}
		if err != nil {
			log.Printf("runtime request rejected uid=%d: %v", request.UID, err)
			endpoint.UID = request.UID
			endpoint.Error = "user runtime unavailable"
			endpoint.Paused = errors.Is(err, backend.ErrPaused)
		}
		if err := encoder.Encode(endpoint); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (broker *broker) acquire(request identity.Request) (gateway.Endpoint, error) {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	if broker.stopping {
		return gateway.Endpoint{}, errors.New("supervisor is stopping")
	}
	if request.UID == broker.gatewayUID {
		return gateway.Endpoint{}, errors.New("service identity cannot open a terminal")
	}
	user, err := identity.Resolve(request)
	if err != nil {
		return gateway.Endpoint{}, err
	}
	if previous := broker.instances[user.UID]; previous != nil {
		if previous.worker.Alive() && reflect.DeepEqual(previous.user, user) {
			return previous.endpoint, nil
		}
		if err := previous.worker.Terminate(time.Duration(broker.config.ShutdownTimeoutSeconds+5) * time.Second); err != nil {
			return gateway.Endpoint{}, err
		}
		delete(broker.instances, user.UID)
	}
	for uid, instance := range broker.instances {
		if !instance.worker.Alive() {
			delete(broker.instances, uid)
		}
	}
	if len(broker.instances) >= broker.config.MaxUsers {
		return gateway.Endpoint{}, errors.New("maximum active user runtimes reached")
	}
	stateDir, err := securefs.UserDirectory(broker.config.StateDir, strconv.FormatUint(uint64(user.UID), 10), user.UID, user.GID)
	if err != nil {
		return gateway.Endpoint{}, err
	}
	runDir, err := securefs.UserDirectory(broker.config.RunDir, strconv.FormatUint(uint64(user.UID), 10), user.UID, user.GID)
	if err != nil {
		return gateway.Endpoint{}, err
	}
	spec := WorkerSpec{Config: broker.config, User: user, StateDir: stateDir, RunDir: runDir}
	data, err := json.Marshal(spec)
	if err != nil {
		return gateway.Endpoint{}, err
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return gateway.Endpoint{}, err
	}
	defer reader.Close()
	defer writer.Close()
	cmd := exec.Command(broker.self, "worker")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	cmd.Dir = user.Home
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{writer}
	cmd.SysProcAttr, err = process.Credential(user.UID, user.GID, user.Groups)
	if err != nil {
		return gateway.Endpoint{}, err
	}
	worker, err := startChild(cmd)
	if err != nil {
		return gateway.Endpoint{}, err
	}
	writer.Close()
	current := &instance{user: user, worker: worker}
	broker.instances[user.UID] = current
	if err := reader.SetReadDeadline(time.Now().Add(time.Duration(broker.config.StartupTimeoutSeconds+5) * time.Second)); err != nil {
		return gateway.Endpoint{}, err
	}
	if err := json.NewDecoder(io.LimitReader(reader, 4096)).Decode(&current.endpoint); err != nil {
		stopErr := worker.Terminate(time.Duration(broker.config.ShutdownTimeoutSeconds+5) * time.Second)
		if stopErr == nil {
			delete(broker.instances, user.UID)
		}
		return gateway.Endpoint{}, fmt.Errorf("user startup failed: %w", err)
	}
	current.endpoint.UID = user.UID
	return current.endpoint, nil
}

func (broker *broker) stop() {
	broker.mu.Lock()
	broker.stopping = true
	instances := make([]*instance, 0, len(broker.instances))
	for _, instance := range broker.instances {
		instances = append(instances, instance)
	}
	broker.mu.Unlock()
	var group sync.WaitGroup
	for _, instance := range instances {
		group.Add(1)
		go func() {
			defer group.Done()
			for instance.worker.Alive() {
				if err := instance.worker.Terminate(time.Duration(broker.config.ShutdownTimeoutSeconds+5) * time.Second); err != nil {
					log.Printf("waiting for daemon save uid=%d", instance.user.UID)
				}
			}
		}()
	}
	group.Wait()
}

func executablePath(appDir, name string) (string, error) {
	path, err := securefs.RootPath(filepath.Join(appDir, "bin", name))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("invalid executable %s", name)
	}
	return path, nil
}
