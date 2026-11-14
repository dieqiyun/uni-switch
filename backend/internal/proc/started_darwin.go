package proc

import (
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

func startTime(pid int) (time.Time, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return time.Time{}, err
	}
	if int(kp.Proc.P_pid) != pid {
		return time.Time{}, errors.New("no such process")
	}
	tv := kp.Proc.P_starttime
	return time.Unix(int64(tv.Sec), int64(tv.Usec)*1000), nil
}
