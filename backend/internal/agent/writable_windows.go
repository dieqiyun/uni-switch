package agent

import "github.com/dieqiyun/uni-switch/backend/internal/update"

// writable says whether this user can write in dir. npm's global folder is
// the user's own (%APPDATA%\npm) by default, but Node.js's own folder
// under Program Files where npm's prefix was left there (新v学员 on
// Discord), which only an administrator can change: Windows says so only
// when a file is made there.
func writable(dir string) bool { return update.Writable(dir) }
