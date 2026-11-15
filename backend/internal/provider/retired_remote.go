package provider

// Legacy remote gateway records stay on disk and are never activated.
func retiredRemotePreset(id string) bool { return id == "remote-uni-switch" || id == "remote-magpie" }
