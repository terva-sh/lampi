package main

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

func processIO() map[string]uint64 {
	if runtime.GOOS != "linux" {
		return nil
	}
	b, err := os.ReadFile("/proc/self/io")
	if err != nil {
		return nil
	}
	out := map[string]uint64{}
	for line := range strings.SplitSeq(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		switch f[0] {
		case "rchar:", "wchar:", "read_bytes:", "write_bytes:":
			n, _ := strconv.ParseUint(f[1], 10, 64)
			out[strings.TrimSuffix(f[0], ":")] = n
		}
	}
	return out
}
