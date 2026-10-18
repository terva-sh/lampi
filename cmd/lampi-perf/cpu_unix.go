//go:build linux || darwin

package main

import "syscall"

func selfCPU() (user, system float64, scope string) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, 0, "unavailable: " + err.Error()
	}
	user = float64(usage.Utime.Sec) + float64(usage.Utime.Usec)/1e6
	system = float64(usage.Stime.Sec) + float64(usage.Stime.Usec)/1e6
	return user, system, "worker RUSAGE_SELF; excludes lake subprocess; sampled before worker exit"
}
