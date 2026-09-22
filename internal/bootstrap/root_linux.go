//go:build linux

package bootstrap

import "os"

func runningAsRoot() bool { return os.Geteuid() == 0 }
