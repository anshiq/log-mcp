//go:build windows

package migrate

func pidAlive(pid int) bool { return false }

func isOurBinary(pid int) bool { return false }

func signalTerm(pid int) error { return nil }
