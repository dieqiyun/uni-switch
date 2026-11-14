//go:build race

package sessions

// raceOn says the tests run under -race, where sync.Pool lets a quarter of
// what is put in it go, so a test of what a pool saves is skipped.
const raceOn = true
