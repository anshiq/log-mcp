//go:build !linux

package process

import "os/exec"

// Resource limits are Linux-first (cgroup v2). On non-Linux platforms the
// child runs unlimited (graceful degradation): no cgroup, no rlimit hook.
func configureResources(cmd *exec.Cmd, l ResourceLimits, procID string) (string, func(), func(), error) {
	return "", func() {}, func() {}, nil
}

func readCgroupUsage(cgroupPath string) (memBytes, cpuNanos int64) { return 0, 0 }

func cgroupOomKilled(cgroupPath string, baseline uint64) bool { return false }

func snapshotOomBaseline(cgroupPath string) uint64 { return 0 }
