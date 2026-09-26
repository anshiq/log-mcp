//go:build !linux
// +build !linux

package process

// TreePIDs is Linux-only (/proc); elsewhere the direct pid is returned.
func TreePIDs(root int) []int { return []int{root} }

// ListeningPortsForPID is Linux-only; elsewhere it reports nothing.
func ListeningPortsForPID(pid int) []int { return nil }
