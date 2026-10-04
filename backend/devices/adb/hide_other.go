//go:build !windows

package adb

import "os/exec"

func setHideWindow(cmd *exec.Cmd) {}
