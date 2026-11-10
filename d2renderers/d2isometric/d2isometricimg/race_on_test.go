//go:build race

package d2isometricimg

// The race detector slows rendering down by an order of magnitude, so tests
// with wall-clock budgets relax them under -race.
const raceEnabled = true
