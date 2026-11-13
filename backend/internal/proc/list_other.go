//go:build !windows

package proc

import "context"

// list asks ps, in its POSIX form, which macOS, Linux and the BSDs all
// take: every process, its id and its whole command line.
func list(ctx context.Context, _ string) ([]Process, error) {
	out, err := CommandContext(ctx, "ps", "-A", "-o", "pid=", "-o", "args=").Output()
	if err != nil {
		return nil, err
	}
	return parseList(string(out)), nil
}

// family is ps with each process's parent too.
func family(ctx context.Context) ([]Kin, error) {
	out, err := CommandContext(ctx, "ps", "-A", "-o", "pid=", "-o", "ppid=", "-o", "args=").Output()
	if err != nil {
		return nil, err
	}
	return parseFamily(string(out)), nil
}

// endFamily ends pid, then what it started as the listing saw it: npx
// leaves the server it ran when it ends.
func endFamily(pid int, kids []int) {
	_ = end(pid)
	for _, k := range kids {
		_ = end(k)
	}
}
