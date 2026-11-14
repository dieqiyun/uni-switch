//go:build race

package gui

// raceOn says the tests run under -race, where sync.Pool lets a quarter of
// what is put in it go (json's encoders, regexp's matchers), so a test of
// how much a path allocates skips its bound.
const raceOn = true
