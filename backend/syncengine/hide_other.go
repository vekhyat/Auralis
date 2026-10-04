//go:build !windows

package syncengine

import "os/exec"

func hideWindow(*exec.Cmd) {}
