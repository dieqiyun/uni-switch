package provider

import "golang.org/x/sys/windows/registry"

// systemZoneName is Windows' name of this computer's time zone ("China
// Standard Time"), which LocalZone can't read there; "" when the registry
// doesn't say.
func systemZoneName() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\TimeZoneInformation`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	name, _, err := k.GetStringValue("TimeZoneKeyName")
	if err != nil {
		return ""
	}
	return name
}
