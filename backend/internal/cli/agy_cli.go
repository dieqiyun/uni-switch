package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// `uni-switch agy run [args…]` is what the function agy uni-switch puts in the
// user's shells runs (agent/agy.go): the agy on PATH, given uni-switch's
// gateway, key and model while settings.json's model is one of uni-switch's,
// and as it is otherwise. Every argument is agy's, --help too. `uni-switch agy
// env` prints those variables and the model, one NAME=value a line and
// nothing off uni-switch, for PowerShell's function.

const agyRunUsage = `uni-switch agy run [args…]   start agy, on uni-switch while its model is one of uni-switch's

The agy on your PATH starts with GEMINI_API_KEY, GOOGLE_GEMINI_BASE_URL and
--model <its model> (not when you give --model yourself); every argument is
agy's. Off uni-switch's models it starts as it is. The function agy uni-switch puts
in your shell's profile runs this.

uni-switch agy env           the variables and model it starts agy with, a NAME=value
                         a line; nothing while agy isn't on uni-switch
`

// agyCmd is `uni-switch agy run|env …`; ok false for another form.
func agyCmd(args []string) (ok bool, err error) {
	if len(args) < 2 || args[0] != "agy" && args[0] != "antigravity-cli" {
		return false, nil
	}
	switch args[1] {
	case "run":
		return true, agyRun(args[2:])
	case "env":
		if len(args) > 2 {
			if slices.Contains([]string{"-h", "--help", "help"}, args[2]) {
				fmt.Print(agyRunUsage)
				return true, nil
			}
			return true, fmt.Errorf("uni-switch agy env takes nothing after it, not %q", args[2])
		}
		env, model := agent.AgyStart()
		if model == "" {
			return true, nil
		}
		for _, kv := range env {
			fmt.Println(kv)
		}
		fmt.Println("model=" + model)
		return true, nil
	}
	return false, nil
}

// agyExec starts the program bin as agy with args and env, in place of
// uni-switch where the system can (agy_exec_*.go); tests set it.
var agyExec = agyExecOS

// agyLook is the agy on PATH; tests set it.
var agyLook = func() (string, error) { return proc.Look("agy") }

func agyRun(args []string) error {
	bin, err := agyLook()
	if err != nil {
		return errors.New("agy isn't installed, or isn't on your PATH")
	}
	if self, err := os.Executable(); err == nil && sameProgram(self, bin) {
		return errors.New("the agy on your PATH is uni-switch itself; install Antigravity CLI")
	}
	env := os.Environ()
	start, model := agent.AgyStart()
	if model != "" {
		env = slices.DeleteFunc(env, func(kv string) bool {
			k, _, _ := strings.Cut(kv, "=")
			return strings.EqualFold(k, "GEMINI_API_KEY") || strings.EqualFold(k, "GOOGLE_GEMINI_BASE_URL")
		})
		env = append(env, start...)
		if !slices.ContainsFunc(args, modelFlag) {
			args = append([]string{"--model", model}, args...)
		}
	}
	return agyExec(bin, args, env)
}

// modelFlag says a is agy's --model, which it takes as -model too.
func modelFlag(a string) bool {
	a, _, _ = strings.Cut(a, "=")
	return a == "--model" || a == "-model"
}

func sameProgram(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}
