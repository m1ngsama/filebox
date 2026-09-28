package main

import (
	"bytes"
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func openPty(t *testing.T) (*os.File, *os.File) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skip(err)
	}
	fd := uintptr(m.Fd())
	var name [128]byte
	for _, req := range []uintptr{unix.TIOCPTYGRANT, unix.TIOCPTYUNLK, unix.TIOCPTYGNAME} {
		if _, _, e := unix.Syscall(unix.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(&name[0]))); e != 0 {
			t.Fatal(e)
		}
	}
	s, err := os.OpenFile(string(name[:bytes.IndexByte(name[:], 0)]), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	return m, s
}
