package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func readPassword(f *os.File) (string, error) {
	fd := int(f.Fd())
	if old, err := unix.IoctlGetTermios(fd, ioctlGetTermios); err == nil {
		t := *old
		t.Lflag &^= unix.ECHO
		if err := unix.IoctlSetTermios(fd, ioctlSetTermios, &t); err != nil {
			return "", err
		}
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		done := make(chan struct{})
		go func() {
			select {
			case <-sig:
				unix.IoctlSetTermios(fd, ioctlSetTermios, old)
				fmt.Fprintln(os.Stderr)
				os.Exit(130)
			case <-done:
			}
		}()
		defer func() {
			signal.Stop(sig)
			close(done)
			unix.IoctlSetTermios(fd, ioctlSetTermios, old)
			fmt.Fprintln(os.Stderr)
		}()
	}
	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
