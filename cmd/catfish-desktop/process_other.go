//go:build !windows

package main

import "os/exec"

func setProcessAttrs(cmd *exec.Cmd) {}
