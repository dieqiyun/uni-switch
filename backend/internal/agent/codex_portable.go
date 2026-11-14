package agent

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// A Codex conversation uni-switch compacted can't go on once Codex talks to
// OpenAI itself, unless its rollout is written portable
// (sessions.PortCodexRollouts, LukeMa on X): done while uni-switch runs, for
// the threads no Codex has open, and again as uni-switch lets Codex go (quit,
// Disconnect), for those it has just let go. Once Codex no longer goes
// through uni-switch, uni-switch's reasoning items are written so too
// (sessions.PortCodexRolloutsLeft): OpenAI won't take a thread holding
// one as the base of its next turn (hentulong on X).

// codexPortEvery is how often a running uni-switch makes the rollouts written
// since portable; codexPortAfter how long after it starts it first does.
var codexPortEvery, codexPortAfter = 10 * time.Minute, 2 * time.Minute

// codexPortUnwire is how long Disconnect spends on it; what is left is
// done by the next pass.
const codexPortUnwire = 3 * time.Second

// portCodexHome makes the rollouts of the Codex home dir portable, for
// up to d; left says Codex no longer goes through uni-switch.
func portCodexHome(dir string, d time.Duration, left bool) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	port := sessions.PortCodexRollouts
	if left {
		port = sessions.PortCodexRolloutsLeft
	}
	res, err := port(ctx, dir)
	if err != nil {
		log.Println("codex rollouts:", err)
	}
	if len(res.Rewritten) > 0 || len(res.Held) > 0 {
		log.Printf("codex rollouts: %d written portable, %d left while a Codex has them open", len(res.Rewritten), len(res.Held))
	}
}

// PortCodexRollouts makes this machine's Codex rollouts portable, for up
// to d: as uni-switch quits.
func PortCodexRollouts(d time.Duration) {
	if home, err := os.UserHomeDir(); err == nil {
		portCodexHome(here(home).codexHome(), d, codexLeft(home))
	}
}

// codexLeft says this machine's Codex doesn't go through uni-switch: neither
// routed to it nor failing over by it.
func codexLeft(home string) bool {
	a := codex(home)
	return !(a.Routed != nil && a.Routed()) && !(a.FailingOver != nil && a.FailingOver())
}

// KeepCodexRolloutsPortable makes this machine's Codex rollouts portable
// every so often until ctx ends.
func KeepCodexRolloutsPortable(ctx context.Context) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := here(home).codexHome()
	wait := codexPortAfter
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = codexPortEvery
		// a pass that is cut short goes on from where it was next time
		portCodexHome(dir, codexPortEvery/2, codexLeft(home))
	}
}
