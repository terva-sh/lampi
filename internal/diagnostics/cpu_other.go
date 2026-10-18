//go:build !linux && !darwin

package diagnostics

func processCPU() (float64, bool) { return 0, false }
