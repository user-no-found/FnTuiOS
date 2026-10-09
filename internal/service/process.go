// Package service supervises the root broker and separate unprivileged user runtimes.
package service

import (
	"github.com/user-no-found/FnPackKit/runtime/process"
	"os/exec"
)

type child = process.Child

func startChild(cmd *exec.Cmd) (*child, error) {
	return process.Start(cmd)
}
