//go:build !race

package layout

// raceEnabled tells the timing tests that the race detector slows them down.
const raceEnabled = false
