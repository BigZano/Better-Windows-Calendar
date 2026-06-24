//go:build !windows

// Package singleinstance is a no-op on non-Windows platforms: there is no mutex
// and no pipe IPC, so every launch behaves as the primary and Forward does
// nothing. The .ics file-association launch path this supports is Windows-only.
package singleinstance

// Acquire always reports primary on non-Windows platforms, with a no-op release.
// onMessage is never invoked because there is no IPC channel to deliver on.
func Acquire(onMessage func(payload string)) (primary bool, release func(), err error) {
	return true, func() {}, nil
}

// Forward is a no-op on non-Windows platforms.
func Forward(payload string) error { return nil }
