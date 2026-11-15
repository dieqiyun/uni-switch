//go:build !windows

package provider

// systemZoneName is "" off Windows, where LocalZone reads the zone.
func systemZoneName() string { return "" }
