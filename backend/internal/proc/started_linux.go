package proc

import (
	"bytes"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// clockTicks is USER_HZ, the unit of /proc/<pid>/stat's starttime: 100 on
// every Linux the kernel's ABI keeps it fixed for.
const clockTicks = 100

func startTime(pid int) (time.Time, error) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return time.Time{}, err
	}
	ticks, err := statStartTicks(b)
	if err != nil {
		return time.Time{}, err
	}
	s, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, err
	}
	boot, err := bootTime(s)
	if err != nil {
		return time.Time{}, err
	}
	return boot.Add(time.Duration(ticks) * time.Second / clockTicks), nil
}

// statStartTicks is field 22 of a /proc/<pid>/stat line, starttime: the
// fields are counted after the command's closing parenthesis, since the
// command itself may hold spaces and parentheses.
func statStartTicks(b []byte) (uint64, error) {
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return 0, errors.New("unreadable /proc stat")
	}
	f := strings.Fields(string(b[i+1:]))
	// f[0] is field 3 (state), so field 22 is f[19]
	if len(f) < 20 {
		return 0, errors.New("unreadable /proc stat")
	}
	return strconv.ParseUint(f[19], 10, 64)
}

// bootTime is /proc/stat's btime.
func bootTime(b []byte) (time.Time, error) {
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(n, 0), nil
		}
	}
	return time.Time{}, errors.New("no btime in /proc/stat")
}
