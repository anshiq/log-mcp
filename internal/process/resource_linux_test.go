//go:build linux

package process_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"agent-runtime/internal/process"
)

// cgroup2 magic number for /sys/fs/cgroup.
const cgroup2Magic = 0x63677270

// requireWritableCgroup2 verifies cgroup v2 is mounted at /sys/fs/cgroup AND a
// process slice can actually be created and limited here (delegated cgroups).
// It returns the scope dir and skips the test otherwise. Without delegation
// (e.g. Docker without systemd) MkdirAll/write fails with EPERM, and the test
// must not fail — the production code degrades to unlimited in exactly that
// case.
func requireWritableCgroup2(t *testing.T) string {
	t.Helper()
	var st unix.Statfs_t
	if err := unix.Statfs("/sys/fs/cgroup", &st); err != nil || st.Type != cgroup2Magic {
		t.Skip("cgroup v2 not mounted at /sys/fs/cgroup")
	}
	scope := filepath.Join("/sys/fs/cgroup", "agent-runtime.scope")
	if err := os.MkdirAll(scope, 0o755); err != nil {
		t.Skipf("cgroup v2 not writable (no delegation?): %v", err)
	}
	probe := filepath.Join(scope, ".write-probe")
	if err := os.WriteFile(probe, []byte("0"), 0o644); err != nil {
		_ = os.Remove(scope)
		t.Skipf("cgroup v2 not writable: %v", err)
	}
	_ = os.Remove(probe)
	t.Cleanup(func() { _ = os.Remove(scope) })
	return scope
}

// memoryLimitStartSpec builds a StartSpec for the helper with a cgroup memory
// limit of limitMB MiB.
func memoryLimitStartSpec(t *testing.T, limitMB int) process.StartSpec {
	t.Helper()
	spec := helperCommand(t, "oom")
	spec.Limits = process.ResourceLimits{MemoryBytes: int64(limitMB) * 1024 * 1024}
	return spec
}

func cgroupSliceFor(t *testing.T, scope, procID string) string {
	// Mirrors internal/process sanitizeID: only [a-z0-9_-] survive.
	var sb strings.Builder
	for _, c := range procID {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			sb.WriteByte(byte(c))
		} else {
			sb.WriteByte('_')
		}
	}
	return filepath.Join(scope, sb.String())
}

// TestCgroupMemoryLimitPlacesAndLimitsProcess exercises the cgroup v2 resource
// path end-to-end: a process started with a memory limit lands in its own
// slice, memory.max is enforced, and an allocation beyond the limit is OOM-killed
// and surfaced as a crash event. Skip-gated on cgroup v2 + delegation.
func TestCgroupMemoryLimitPlacesAndLimitsProcess(t *testing.T) {
	scope := requireWritableCgroup2(t)

	m := process.New(context.Background(), process.Options{})
	defer m.Shutdown(5 * time.Second)

	// A 32 MiB limit is comfortably below the OOM helper's 256 MiB ceiling, so
	// the kernel must kill it within this slice.
	const limitMB = 32
	proc, err := m.Start(context.Background(), memoryLimitStartSpec(t, limitMB), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	info := proc.Info()
	if info.Status != process.StatusRunning || info.PID <= 0 {
		t.Fatalf("info = %+v, want running", info)
	}

	slice := cgroupSliceFor(t, scope, proc.ID)
	// The slice must exist and carry the requested cap.
	limitBytes, err := os.ReadFile(filepath.Join(slice, "memory.max"))
	if err != nil {
		t.Fatalf("memory.max unreadable (slice %s not created?): %v", slice, err)
	}
	if got := strings.TrimSpace(string(limitBytes)); got != strconv.Itoa(limitMB*1024*1024) {
		t.Fatalf("memory.max = %s, want %d", got, limitMB*1024*1024)
	}

	// The process touches memory beyond the cap: it must be OOM-killed, and the
	// crash must be visible as a crashed/exited status with a non-zero exit.
	select {
	case <-proc.Done():
	case <-time.After(20 * time.Second):
		mem, _ := proc.LiveUsage()
		t.Fatalf("process not killed within 20s; memory.max=%s memusage=%d", string(limitBytes), mem)
	}
	info = proc.Info()
	if info.Status != process.StatusExited && info.Status != process.StatusCrashed {
		t.Fatalf("status after OOM = %q, want exited/crashed", info.Status)
	}
	if info.ExitCode == nil || *info.ExitCode == 0 {
		t.Fatalf("OOM-killed exit code = %v, want non-zero", info.ExitCode)
	}
}

// TestCgroupLiveUsageReadsSliceMetrics verifies the live usage probe reads real
// memory numbers from the slice while a process runs (the data feeding
// process_status / runtime_stats resource reporting).
func TestCgroupLiveUsageReadsSliceMetrics(t *testing.T) {
	scope := requireWritableCgroup2(t)

	m := process.New(context.Background(), process.Options{})
	defer m.Shutdown(5 * time.Second)
	// A generous limit so the process stays alive; only usage reading is tested.
	proc, err := m.Start(context.Background(), memoryLimitStartSpec(t, 512), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	if info := proc.Info(); info.Status != process.StatusRunning {
		t.Fatalf("info = %+v, want running", info)
	}
	waitFor(t, 10*time.Second, "TOUCHING line", func() bool {
		for _, e := range allStdout(proc) {
			if strings.HasPrefix(e.Line, "TOUCHING") {
				return true
			}
		}
		return false
	})
	mem, cpu := proc.LiveUsage()
	if mem <= 0 {
		t.Fatalf("LiveUsage mem = %d, want > 0 (slice %s)", mem, cgroupSliceFor(t, scope, proc.ID))
	}
	if cpu < 0 {
		t.Fatalf("LiveUsage cpu = %d, want >= 0", cpu)
	}
	if err := m.Stop(context.Background(), proc.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
