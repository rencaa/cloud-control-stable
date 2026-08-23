//go:build !windows
// +build !windows

package main

// Linux and other non-Windows builds do not provide a native system tray.
// Keep the server entry point portable while leaving the Windows tray intact.
func runTray(_ string, _ func(), _ func()) {}
