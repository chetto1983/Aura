//go:build race

package docfields

// raceSlowdown widens a timing budget under the race detector by the top of the 2-20x
// execution-time overhead its documentation states (go.dev/doc/articles/race_detector). The
// pathological-pattern pass measured 19x locally and 10.66 s on a GitHub runner (2026-10-21).
const raceSlowdown = 20
