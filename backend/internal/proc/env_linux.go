package proc

import (
	"context"
	"os"
	"strconv"
)

func envOf(_ context.Context, pid int, names []string) (map[string]string, error) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return nil, err
	}
	return parseEnviron(b, names), nil
}
