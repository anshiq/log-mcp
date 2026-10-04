package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	fmt.Fprintln(os.Stderr, "agent-runtime-gui is deprecated: use `agent-runtime gui` instead")
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent-runtime-gui:", err)
		os.Exit(1)
	}
	rt := filepath.Join(filepath.Dir(exe), "agent-runtime")
	if _, err := os.Stat(rt); err != nil {
		rt = "agent-runtime"
	}
	args := append([]string{"gui"}, os.Args[1:]...)
	cmd := exec.Command(rt, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "agent-runtime-gui:", err)
		os.Exit(1)
	}
}
