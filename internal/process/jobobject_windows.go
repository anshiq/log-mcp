//go:build windows

// Windows process-tree termination via Job Objects.
//
// On Unix the whole process tree is signalled as a unit via Setpgid + negative
// kill(2). Windows has no process groups; the equivalent primitive is a Job
// Object with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, which terminates every
// process assigned to the job the moment the job's last handle is closed.
//
// This file is not wired into the process manager directly (the orchestrator
// does that). Use it like so:
//
//	// in startInstance, immediately AFTER cmd.Start() succeeds and BEFORE
//	// returning (and before spawning the readers/waiters):
//	cleanup, err := setupJob(cmd)
//	if err != nil {
//		// Optionally fail the start, or log and continue without tree-kill.
//	}
//	inst.jobCleanup = cleanup // close it when the instance is torn down
//
//	// in process-group termination (the Windows counterpart of signalGroup):
//	err := terminateJobTree(uint32(cmd.Process.Pid))
//
// The returned cleanup closes the job's last handle, which (because
// KILL_ON_JOB_CLOSE is set) terminates every process still assigned to the job
// as a side effect; call it during instance teardown. terminateJobTree is the
// explicit, mid-run variant used to kill the whole tree on demand.
package process

import (
	"fmt"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobByPID tracks the open job handle for every process started through
// setupJob, keyed by the child's OS pid. Holding a handle open prevents
// KILL_ON_JOB_CLOSE from firing until we intentionally release it, and lets
// terminateJobTree find the job for a given pid.
var (
	jobMu    sync.Mutex
	jobByPID = make(map[uint32]windows.Handle)
)

// setupJob creates a Job Object configured with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
// and assigns cmd's already-started child process to it. It must be called
// AFTER cmd.Start() has returned (cmd.Process must be non-nil). Any descendant
// process created after the assignment is automatically captured by the job.
//
// The returned cleanup releases the job: it removes the pid from the registry
// and closes the job's last handle. Because KILL_ON_JOB_CLOSE is set, closing
// that last handle terminates every process still assigned to the job — call it
// during instance teardown so an abandoned tree cannot survive the daemon.
func setupJob(cmd *exec.Cmd) (cleanup func(), err error) {
	if cmd == nil || cmd.Process == nil {
		return nil, fmt.Errorf("setupJob: cmd.Start() must be called first")
	}
	pid := uint32(cmd.Process.Pid)

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}

	// KILL_ON_JOB_CLOSE: when the job's last handle is closed, all processes
	// assigned to the job are terminated. This mirrors the Unix group-kill.
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("set job object limits: %w", err)
	}

	// Assign the child to the job. AssignProcessToJobObject needs a process
	// handle opened with PROCESS_SET_QUOTA and PROCESS_TERMINATE access.
	proc, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		pid,
	)
	if err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("open process %d: %w", pid, err)
	}
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		windows.CloseHandle(proc)
		windows.CloseHandle(job)
		return nil, fmt.Errorf("assign process %d to job: %w", pid, err)
	}
	// The job holds its own reference to the process; the temporary handle is
	// no longer needed.
	windows.CloseHandle(proc)

	// Register the job handle so terminateJobTree can find it by pid. Keep this
	// handle open (rather than handing ownership back to the caller) so
	// KILL_ON_JOB_CLOSE only fires when we deliberately release the job.
	jobMu.Lock()
	jobByPID[pid] = job
	jobMu.Unlock()

	return func() {
		jobMu.Lock()
		if _, ok := jobByPID[pid]; ok {
			delete(jobByPID, pid)
		}
		jobMu.Unlock()
		// Closing the last handle terminates any processes still in the job.
		windows.CloseHandle(job)
	}, nil
}

// terminateJobTree terminates every process assigned to the job that the given
// child pid was started under. It is the Windows counterpart of signalGroup:
// call it during process-group termination (e.g. the SIGKILL escalation in
// stopInstance) instead of signalling only the direct child.
//
// It returns an error if no job was registered for pid (for example a process
// started before setupJob was wired in, or on a non-Windows build where the
// stub returns nil). Use the returned cleanup (from setupJob) to release the
// job handle afterwards.
func terminateJobTree(pid uint32) error {
	jobMu.Lock()
	job, ok := jobByPID[pid]
	jobMu.Unlock()
	if !ok {
		return fmt.Errorf("no job object registered for pid %d", pid)
	}
	return windows.TerminateJobObject(job, 1)
}
