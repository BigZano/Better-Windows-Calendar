//go:build windows

package singleinstance

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// setNames points the package's mutex/pipe at unique per-test identities so a
// real running PyCalendar instance (or a parallel test) cannot collide. It
// restores the originals via t.Cleanup.
func setNames(t *testing.T) {
	t.Helper()
	origMutex, origPipe := mutexName, pipeName
	suffix := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	mutexName = `Local\PyCalendar-SingleInstance-Test-` + suffix
	pipeName = `\\.\pipe\PyCalendar-Test-` + suffix
	t.Cleanup(func() {
		mutexName, pipeName = origMutex, origPipe
	})
}

func TestAcquireForwardRoundTrip(t *testing.T) {
	setNames(t)

	got := make(chan string, 1)
	primary, release, err := Acquire(func(p string) { got <- p })
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !primary {
		t.Fatal("expected to be primary (no other instance under unique names)")
	}
	if release == nil {
		t.Fatal("primary returned nil release")
	}
	defer release()

	const path = `C:\tmp\x.ics`
	if err := Forward(path); err != nil {
		t.Fatalf("Forward: %v", err)
	}

	select {
	case msg := <-got:
		if msg != path {
			t.Errorf("onMessage got %q, want %q", msg, path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for forwarded payload")
	}
}

func TestSecondAcquireIsSecondary(t *testing.T) {
	setNames(t)

	primary, release, err := Acquire(nil)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if !primary {
		t.Fatal("first Acquire should be primary")
	}
	defer release()

	// A second Acquire while the first holds the mutex must report secondary.
	secondaryPrimary, secondaryRelease, err := Acquire(nil)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if secondaryPrimary {
		t.Error("second Acquire should be secondary while primary holds the mutex")
	}
	if secondaryRelease != nil {
		secondaryRelease()
	}
}

func TestForwardEmptyPayloadIsNoop(t *testing.T) {
	setNames(t)
	// No listener exists; an empty payload must not attempt to dial.
	if err := Forward("   "); err != nil {
		t.Errorf("Forward(empty) = %v, want nil", err)
	}
}
