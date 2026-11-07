package supplier

import "github.com/dieqiyun/uni-switch/backend/internal/modelmeta"

// Test adapters; the production path and these wrappers share one parser.
func profile(m object) map[string]any {
	return modelmeta.Resolve(modelmeta.ParseVendor(str(m["id"]), m), nil, modelmeta.Preferences{}, "").Profile
}
func capabilities(m object) map[string]any {
	return modelmeta.Resolve(modelmeta.ParseVendor(str(m["id"]), m), nil, modelmeta.Preferences{}, "").Capabilities
}
