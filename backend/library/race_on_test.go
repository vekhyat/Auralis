//go:build race

package library

// raceEnabled is true when the test binary is built with -race, which slows
// CPU-bound code several-fold; timing bounds are skipped under it.
const raceEnabled = true
