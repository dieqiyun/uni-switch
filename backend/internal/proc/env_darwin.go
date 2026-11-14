package proc

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

func envOf(ctx context.Context, pid int, names []string) (map[string]string, error) {
	out, err := CommandContext(ctx, "ps", "-E", "-ww", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return nil, err
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return nil, errors.New("no such process")
	}
	return parsePSEnv(" "+line, names), nil
}
