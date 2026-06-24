//go:build windows

// Package singleinstance enforces a single running tray instance on Windows and
// forwards launch arguments (an .ics path from a file-association double-click)
// to the already-running instance.
//
// Two primitives cooperate:
//
//   - A named mutex is the single-instance authority. Whoever creates it first
//     is the primary; everyone else sees ERROR_ALREADY_EXISTS and is a
//     secondary. The mutex alone settles primary-vs-secondary because exactly
//     one creator can win, whereas a pipe listener is not exclusive (several
//     processes could each open their own listener) and so cannot prove
//     single-instance on its own.
//   - A named pipe is the argument channel. The primary listens; a secondary
//     dials it, writes its payload, and exits.
//
// The mutex uses the Local\ namespace (per-session) on purpose: single-instance
// is scoped to the interactive desktop session, which avoids cross-session
// collisions on multi-user / RDP machines where a Global\ mutex would let one
// user's tray steal another's launch.
package singleinstance

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// mutexName and pipeName are the fixed identities of the two IPC primitives.
// They are package variables (not consts) so tests can point a round-trip at
// unique names and avoid colliding with a real running instance. The public API
// (Acquire/Forward) stays parameter-free.
var (
	mutexName = `Local\PyCalendar-SingleInstance`
	pipeName  = `\\.\pipe\PyCalendar`
)

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
)

// dialTimeout bounds how long a secondary waits to hand its payload to the
// primary before giving up.
const dialTimeout = 2 * time.Second

// Acquire claims single-instance ownership for the current process.
//
// If this process is the primary (it created the mutex), it starts a pipe
// listener that calls onMessage for every payload a secondary forwards, and
// returns primary=true with a release func that tears down the listener and the
// mutex. If another instance already holds the mutex, it returns primary=false;
// the caller should Forward any payload and exit.
func Acquire(onMessage func(payload string)) (primary bool, release func(), err error) {
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return false, nil, fmt.Errorf("encode mutex name: %w", err)
	}

	// CreateMutexW returns a handle whether or not the mutex already existed;
	// GetLastError (returned here as the third Call result) distinguishes them.
	handle, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	mutex := windows.Handle(handle)
	if mutex == 0 {
		return false, nil, fmt.Errorf("create mutex: %v", callErr)
	}

	if ce, ok := callErr.(windows.Errno); ok && ce == windows.ERROR_ALREADY_EXISTS {
		// Secondary: another instance owns the mutex. Drop our handle and bow out.
		windows.CloseHandle(mutex)
		return false, nil, nil
	}

	// Primary: we created the mutex. Stand up the pipe listener.
	listener, err := winio.ListenPipe(pipeName, nil)
	if err != nil {
		windows.CloseHandle(mutex)
		return false, nil, fmt.Errorf("listen pipe: %w", err)
	}

	go serve(listener, onMessage)

	var once sync.Once
	release = func() {
		once.Do(func() {
			listener.Close()
			windows.CloseHandle(mutex)
		})
	}
	return true, release, nil
}

// serve accepts pipe connections until the listener is closed, reading one
// newline-delimited payload per connection and dispatching non-empty payloads
// to onMessage.
func serve(listener net.Listener, onMessage func(payload string)) {
	for {
		c, err := listener.Accept()
		if err != nil {
			// Listener closed (release) — stop serving.
			return
		}
		handleConn(c, onMessage)
	}
}

func handleConn(c net.Conn, onMessage func(payload string)) {
	defer c.Close()
	line, _ := bufio.NewReader(c).ReadString('\n')
	payload := strings.TrimSpace(line)
	if payload == "" || onMessage == nil {
		return
	}
	onMessage(payload)
}

// Forward dials the primary instance's pipe and writes the payload, newline
// terminated. Empty payloads are a no-op. Used by a secondary to hand its .ics
// path to the running instance.
func Forward(payload string) error {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil
	}
	timeout := dialTimeout
	c, err := winio.DialPipe(pipeName, &timeout)
	if err != nil {
		return fmt.Errorf("dial pipe: %w", err)
	}
	defer c.Close()
	if _, err := c.Write([]byte(payload + "\n")); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
}
