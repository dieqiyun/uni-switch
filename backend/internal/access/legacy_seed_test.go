package access

import "github.com/dieqiyun/uni-switch/backend/internal/settings"

// Seed an old credential for compatibility tests without a sharing API.
func seedLegacyDefault() error {
	s := settings.Load()
	s.LANKey = "sk-uni-switch-legacy-default-fixture"
	if err := settings.Save(s); err != nil {
		return err
	}
	return MigrateLegacyLANKey()
}
