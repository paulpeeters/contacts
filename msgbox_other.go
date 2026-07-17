//go:build !windows

package main

import "log"

// showFatalError is a no-op message box replacement on non-Windows
// platforms (this app targets Windows, but keeping this build tag pair
// means the source still compiles elsewhere for a quick sanity check).
func showFatalError(title, message string) {
	log.Printf("[%s] %s", title, message)
}
