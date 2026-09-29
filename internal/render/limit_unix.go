//go:build unix

package render

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

const backstop = 30 * time.Second

var cpuSeconds uint64 = 5

const cpuExit = 75

// The Go runtime ignores SIGXCPU unless notified, and macOS never sends the hard-limit SIGKILL.
func limitCPU() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGXCPU)
	go func() {
		<-c
		os.Exit(cpuExit)
	}()
	syscall.Setrlimit(syscall.RLIMIT_CPU, &syscall.Rlimit{Cur: cpuSeconds, Max: cpuSeconds + 1})
}

// SIGKILL here is the hard RLIMIT_CPU; the caller rules out its own kills first.
func cpuKilled(err error) bool {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	ws, ok := ee.Sys().(syscall.WaitStatus)
	return ee.ExitCode() == cpuExit || ok && ws.Signaled() && (ws.Signal() == syscall.SIGXCPU || ws.Signal() == syscall.SIGKILL)
}
