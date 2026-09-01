//go:build linux

package process

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// cgroup2 magic for /sys/fs/cgroup.
const cgroup2Magic = 0x63677270

// cgroupScope is the cgroup v2 parent under which per-process slices are
// created: /sys/fs/cgroup/agent-runtime.scope/<process_id>.
const cgroupScope = "agent-runtime.scope"

// configureResources places the child in a per-process cgroup v2 slice with the
// requested memory/cpu limits. It sets cmd.SysProcAttr.CgroupFD/UseCgroupFD so
// the child is placed in the slice at exec. On any failure it returns a
// non-nil error so the caller can warn and run unlimited (graceful
// degradation). closeFD must be called after cmd.Start; cleanup after the
// process exits (removes the slice).
func configureResources(cmd *exec.Cmd, l ResourceLimits, procID string) (cgroupPath string, closeFD, cleanup func(), err error) {
	noop := func() {}
	if !l.Enabled() {
		return "", noop, noop, nil
	}
	root := "/sys/fs/cgroup"
	if !isCgroup2(root) {
		return "", nil, nil, fmt.Errorf("cgroup v2 not available at %s", root)
	}
	dir := filepath.Join(root, cgroupScope, sanitizeID(procID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, nil, fmt.Errorf("create cgroup %s: %w", dir, err)
	}
	cleanup = func() { _ = os.Remove(dir) }

	if l.MemoryBytes > 0 {
		if err := os.WriteFile(filepath.Join(dir, "memory.max"), []byte(strconv.FormatInt(l.MemoryBytes, 10)), 0o644); err != nil {
			cleanup()
			return "", nil, nil, fmt.Errorf("set memory.max: %w", err)
		}
		high := l.MemoryBytes * 9 / 10
		if high < 1 {
			high = 1
		}
		_ = os.WriteFile(filepath.Join(dir, "memory.high"), []byte(strconv.FormatInt(high, 10)), 0o644)
	}
	if l.CPUQuota > 0 {
		period := int64(100000) // 100ms in microseconds
		quota := int64(l.CPUQuota * float64(period))
		if quota < 1000 {
			quota = 1000 // at least 1ms, or the kernel rejects it
		}
		if err := os.WriteFile(filepath.Join(dir, "cpu.max"), []byte(fmt.Sprintf("%d %d", quota, period)), 0o644); err != nil {
			cleanup()
			return "", nil, nil, fmt.Errorf("set cpu.max: %w", err)
		}
	}

	fd, err := unix.Open(dir, unix.O_DIRECTORY|unix.O_RDONLY, 0)
	if err != nil {
		cleanup()
		return "", nil, nil, fmt.Errorf("open cgroup: %w", err)
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CgroupFD = fd
	cmd.SysProcAttr.UseCgroupFD = true
	closeFD = func() { _ = unix.Close(fd) }
	return dir, closeFD, cleanup, nil
}

func isCgroup2(path string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return false
	}
	return st.Type == cgroup2Magic
}

// sanitizeID makes a process id safe as a cgroup directory name.
func sanitizeID(id string) string {
	out := make([]byte, 0, len(id))
	for _, c := range id {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			out = append(out, byte(c))
		} else {
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "proc"
	}
	return string(out)
}

// readCgroupUsage returns the live memory bytes and CPU usage (nanoseconds)
// for a cgroup slice, or zeros when unavailable.
func readCgroupUsage(cgroupPath string) (memBytes int64, cpuNanos int64) {
	if cgroupPath == "" {
		return 0, 0
	}
	if b, err := os.ReadFile(filepath.Join(cgroupPath, "memory.current")); err == nil {
		if v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil {
			memBytes = v
		}
	}
	if b, err := os.ReadFile(filepath.Join(cgroupPath, "cpu.stat")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "usage_usec ") {
				if v, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "usage_usec ")), 10, 64); err == nil {
					cpuNanos = v * 1000 // usec -> nanos
				}
				break
			}
		}
	}
	return memBytes, cpuNanos
}

// cgroupOomKilled reports whether the cgroup's oom_kill counter moved past the
// baseline since start (i.e. the kernel killed a process in this slice for
// exceeding its memory limit).
func cgroupOomKilled(cgroupPath string, baseline uint64) bool {
	if cgroupPath == "" {
		return false
	}
	f, err := os.Open(filepath.Join(cgroupPath, "memory.events"))
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "oom_kill ") {
			if v, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "oom_kill ")), 10, 64); err == nil {
				return v > baseline
			}
		}
	}
	return false
}

// snapshotOomBaseline reads the current oom_kill counter of a cgroup slice.
func snapshotOomBaseline(cgroupPath string) uint64 {
	if cgroupPath == "" {
		return 0
	}
	f, err := os.Open(filepath.Join(cgroupPath, "memory.events"))
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "oom_kill ") {
			if v, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "oom_kill ")), 10, 64); err == nil {
				return v
			}
		}
	}
	return 0
}
