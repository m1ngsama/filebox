package main

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadPasswordNoEchoOnTTY(t *testing.T) {
	m, s := openPty(t)
	defer m.Close()
	defer s.Close()
	type res struct {
		pw  string
		err error
	}
	c := make(chan res, 1)
	go func() {
		pw, err := readPassword(s)
		c <- res{pw, err}
	}()
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		tio, err := unix.IoctlGetTermios(int(s.Fd()), ioctlGetTermios)
		if err != nil {
			t.Fatal(err)
		}
		if tio.Lflag&unix.ECHO == 0 || time.Now().After(deadline) {
			break
		}
	}
	if _, err := m.Write([]byte("secret-pw\n")); err != nil {
		t.Fatal(err)
	}
	r := <-c
	if r.err != nil || r.pw != "secret-pw" {
		t.Fatalf("got %q, %v", r.pw, r.err)
	}
	tio, err := unix.IoctlGetTermios(int(s.Fd()), ioctlGetTermios)
	if err != nil || tio.Lflag&unix.ECHO == 0 {
		t.Fatalf("echo not restored: %v", err)
	}
	s.Write([]byte("END\n"))
	out, _ := bufio.NewReader(m).ReadString('D')
	if strings.Contains(out, "secret") {
		t.Fatalf("password echoed: %q", out)
	}
}

func TestReadPasswordPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("piped-pw\r\n"))
	w.Close()
	if pw, err := readPassword(r); err != nil || pw != "piped-pw" {
		t.Fatalf("got %q, %v", pw, err)
	}
}
