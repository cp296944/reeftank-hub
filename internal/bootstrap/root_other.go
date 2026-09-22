//go:build !linux

package bootstrap

func runningAsRoot() bool { return false }
