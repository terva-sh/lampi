//go:build !linux

package diagnostics

func resident() uint64 { return 0 }
