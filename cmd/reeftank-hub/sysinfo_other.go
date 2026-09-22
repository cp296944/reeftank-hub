//go:build !linux

package main

func diskKB(path string) (total, free int) { return 0, 0 }
