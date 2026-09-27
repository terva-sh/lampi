//go:build race

package catalog

// raceEnabled is true under -race, which slows the pure-Go SQLite
// driver enough that read timings say nothing about the lake.
const raceEnabled = true
