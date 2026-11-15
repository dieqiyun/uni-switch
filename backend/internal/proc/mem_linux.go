package proc

import (
	"os"
	"strings"
)

func memTotal() int64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "MemTotal:" {
			n, _ := parseKiB(f[1])
			return n
		}
	}
	return 0
}
