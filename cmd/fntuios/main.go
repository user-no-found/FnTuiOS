// Command fntuios supervises authenticated per-user terminal services on fnOS.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"fntuios/internal/config"
	"fntuios/internal/gateway"
	"fntuios/internal/service"
)

// Build metadata is supplied from the project.json authority.
var version, displayName, appID string

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Printf("fntuios: %v", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: fntuios serve|configure|prepare-state|check-config|version")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	switch args[0] {
	case "version":
		if version == "" {
			fmt.Println("FnTuiOS development build")
			return nil
		}
		fmt.Printf("%s %s (%s)\n", displayName, version, appID)
		return nil
	case "serve", "check-config", "health", "prepare-state":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		path := flags.String("config", "", "editable deployment configuration")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *path == "" || flags.NArg() != 0 {
			return errors.New("--config FILE is required")
		}
		if args[0] == "check-config" {
			_, err := config.Load(*path)
			return err
		}
		if args[0] == "health" {
			return service.Health(*path)
		}
		if args[0] == "prepare-state" {
			return service.PrepareState(*path)
		}
		return service.Serve(ctx, *path)
	case "configure":
		return configure(args[1:])
	case "gateway":
		return runGateway(ctx)
	case "worker":
		var spec service.WorkerSpec
		if err := decodeInput(&spec); err != nil {
			return err
		}
		report := os.NewFile(3, "worker-report")
		defer report.Close()
		return service.RunWorker(ctx, spec, report)
	default:
		return errors.New("unknown command")
	}
}

func configure(args []string) error {
	flags := flag.NewFlagSet("configure", flag.ContinueOnError)
	template := flags.String("template", "", "packaged gateway.json")
	output := flags.String("output", "", "root-owned editable gateway.json")
	appDir := flags.String("app-dir", "", "package target directory")
	stateDir := flags.String("state-dir", "", "root-owned user-state parent")
	runDir := flags.String("run-dir", "", "root-owned runtime parent")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *template == "" || *output == "" || flags.NArg() != 0 {
		return errors.New("--template and --output are required")
	}
	return config.Configure(*template, *output, *appDir, *stateDir, *runDir)
}

func runGateway(ctx context.Context) error {
	if os.Geteuid() == 0 {
		return errors.New("gateway must run unprivileged")
	}
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, 38, 1, 0, 0, 0, 0); errno != 0 {
		return fmt.Errorf("set no-new-privileges: %w", errno)
	}
	var options gateway.Options
	if err := decodeInput(&options); err != nil {
		return err
	}
	options.Name = displayName
	listenerFile := os.NewFile(3, "gateway-listener")
	defer listenerFile.Close()
	brokerFile := os.NewFile(4, "gateway-broker")
	defer brokerFile.Close()
	listener, err := net.FileListener(listenerFile)
	if err != nil {
		return err
	}
	defer listener.Close()
	broker, err := net.FileConn(brokerFile)
	if err != nil {
		return err
	}
	defer broker.Close()
	return gateway.Serve(ctx, listener, broker, options)
}

func decodeInput(value any) error {
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 65536))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}
