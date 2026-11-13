package provider

// Warm up now (a Windows user on Discord): one account's warm-up sent at
// once from Settings › Warm-up and check-in, whatever the warm-up settings
// say. It is the request the scheduled warm-up sends — warmCodexLogin for
// a ChatGPT account, Claude Code's own run (WarmClaudeVia) for a Claude
// one — to that account alone, and what it says back is the result. The
// scheduled warm-up's file is left to the scheduled one: its next look
// reads the window as started, from its own read.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// warmNowWait is how long Warm up now waits on its request, as long as
// the scheduled warm-up gives one look.
const warmNowWait = 2 * time.Minute

// claudeWarmSend is a Claude warm-up's send, Claude Code asked with
// configDir the config directory of a saved account, "" for the one it is
// signed in to. The gateway, which runs Claude Code, sets it (WarmClaudeVia).
var claudeWarmSend func(ctx context.Context, configDir string) error

// WarmClaudeVia sets how a Claude account's warm-up runs Claude Code.
func WarmClaudeVia(f func(ctx context.Context, configDir string) error) { claudeWarmSend = f }

// WarmNow sends agent's ("codex" or "claude") account user the warm-up's
// one tiny request now, and says why it failed. Once it went, what the
// account has left is read again, as after a scheduled warm-up.
func WarmNow(ctx context.Context, agent, user string) error {
	var name string
	switch agent {
	case "codex":
		name = "ChatGPT"
	case "claude":
		name = "Claude"
	default:
		return fmt.Errorf("%q has no warm-up", agent)
	}
	ls := Logins(agent)
	i := slices.IndexFunc(ls, func(l Login) bool { return strings.EqualFold(l.User, user) })
	if user == "" || i < 0 {
		return fmt.Errorf("no %s account %q", name, user)
	}
	if ls[i].Lapsed != "" {
		// as the scheduled warm-up leaves it out (codexWarmUsage)
		return fmt.Errorf("%s needs signing in again: %s", ls[i].User, ls[i].Lapsed)
	}
	ctx, cancel := context.WithTimeout(ctx, warmNowWait)
	defer cancel()
	if agent == "codex" {
		if err := warmCodexLogin(ctx, ls[i].User); err != nil {
			return err
		}
		StaleAllowance("codex", ls[i].User)
		return nil
	}
	if claudeWarmSend == nil {
		return errors.New("Claude Code isn't available to send the warm-up")
	}
	if err := warmClaudeLogin(claudeWarmSend)(ctx, ls[i].User); err != nil {
		return err
	}
	claudeWarmedNow(ls[i].User)
	return nil
}
