//go:build !linux && !darwin

package main

func selfCPU() (user, system float64, scope string) {
	return 0, 0, "unavailable: agent-only CPU requires Linux or macOS"
}
