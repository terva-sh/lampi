//go:build linux || darwin

package diagnostics

import "syscall"

func processCPU() (float64, bool) {
	var u syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &u); err != nil {
		return 0, false
	}
	return float64(u.Utime.Sec+u.Stime.Sec) + float64(u.Utime.Usec+u.Stime.Usec)/1e6, true
}
