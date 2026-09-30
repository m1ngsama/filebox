package index

import (
	"runtime"

	"golang.org/x/sys/unix"
)

// Linux nice values are per thread, so the goroutine keeps this thread, and the runtime discards it when the goroutine exits.
func lowPriority() {
	runtime.LockOSThread()
	unix.Setpriority(unix.PRIO_PROCESS, unix.Gettid(), 19)
}
