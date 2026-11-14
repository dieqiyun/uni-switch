//go:build !darwin && !linux

package proc

import "context"

func envOf(context.Context, int, []string) (map[string]string, error) { return nil, ErrNoEnv }
