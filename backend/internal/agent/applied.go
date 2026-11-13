package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/lastgood"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// An agent's config is a file anyone can write: another switcher, an
// installer, the agent's own setup. One that takes uni-switch out leaves the
// row showing a uni-switch model while the agent asks its own vendor for it, and
// the user blames uni-switch. So uni-switch remembers what it last set on each agent
// (applied.json, beside the stash) and says when that no longer holds —
// Drift — with the way to set it again.

var appliedMu sync.Mutex

// applied is what uni-switch last set on one agent, and when.
type applied struct {
	At     time.Time         `json:"at"`
	Fields map[string]string `json:"fields"` // field key → value
	// Kept: the user took the agent's config as it was (Keep) while uni-switch
	// had it set up, which still says uni-switch set it up (setUp)
	Kept bool `json:"kept,omitempty"`
}

func appliedPath() string { return filepath.Join(filepath.Dir(provider.Path()), "applied.json") }

// appliedLoad is agent id → what uni-switch set on it.
func appliedLoad() map[string]applied {
	out := map[string]applied{}
	if b, err := os.ReadFile(appliedPath()); err == nil {
		json.Unmarshal(b, &out)
	}
	return out
}

// appliedSave writes m as applied.json. It holds what uni-switch set on every
// agent, and appliedLoad reads a file that doesn't parse as empty: such a
// file is copied aside first, and one that can't be copied isn't written
// over (#2263).
func appliedSave(m map[string]applied) {
	if err := lastgood.KeepUnparsed(appliedPath(), lastgood.Into[map[string]applied]); err != nil {
		log.Printf("applied.json wasn't written over: %v", err)
		return
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	os.MkdirAll(filepath.Dir(appliedPath()), 0o755)
	os.WriteFile(appliedPath(), b, 0o600)
}

func appliedOf(id string) applied {
	appliedMu.Lock()
	defer appliedMu.Unlock()
	return appliedLoad()[id]
}

// record keeps what a field reads after uni-switch set it; a field put back to
// the agent's default is uni-switch's no longer.
func record(id, key, v string) {
	appliedMu.Lock()
	defer appliedMu.Unlock()
	m := appliedLoad()
	a := m[id]
	if a.Fields == nil {
		a.Fields = map[string]string{}
	}
	if v == "" {
		delete(a.Fields, key)
	} else {
		a.Fields[key] = v
	}
	a.At = time.Now()
	m[id] = a
	appliedSave(m)
}

// Apply sets one of the agent's fields and remembers it as uni-switch's, so it
// can be told apart from what something else writes there later. Every
// setting of a field by the user goes through here. A field uni-switch set
// before that this one moves too is uni-switch's doing as well: Claude Code's
// Default takes its tiers and subagent model out with uni-switch's endpoint,
// which read as changed outside uni-switch, and the drift kept the row up
// among the connected ones while it said Not connected (#834).
func (a *Agent) Apply(key, v string) error {
	if a.Native != nil {
		return a.Native.Apply(key, v)
	}
	f := a.Field(key)
	if f == nil {
		return nil
	}
	was := a.Values()
	if err := f.Set(v); err != nil {
		return err
	}
	now := a.Values()
	for k := range appliedOf(a.ID).Fields {
		if k != f.Key && now[k] != was[k] {
			record(a.ID, k, now[k])
		}
	}
	record(a.ID, f.Key, f.Get())
	// uni-switch picked in by the user's own hand (uni-switch model, a pick in
	// the CLI) connects it again
	if a.Wired() {
		forget(a.ID + disconnectedKey)
	}
	return nil
}

// Drift is how an agent differs from what uni-switch set on it.
type Drift struct {
	// Kind says what is off:
	//   "unwired"  the model is uni-switch's but the config no longer sends it
	//              through uni-switch (Check);
	//   "replaced" a uni-switch model uni-switch set was replaced by the agent's own;
	//   "bypassed" the config is right, yet the agent was used since and
	//              nothing of it reached the gateway — it runs on an old
	//              config, or something outside the file overrides it;
	//   "unreachable" the config is right, but the address off loopback
	//              it names the gateway at doesn't answer (#1013);
	//   "proxied"  the config is right, but the agent sends its requests
	//              for the gateway through a proxy (Addr) that doesn't
	//              pass them on to it (codex_proxy.go).
	Kind   string `json:"kind"`
	Field  string `json:"field"`         // the field it shows on
	Now    string `json:"now,omitempty"` // what that field says now
	Want   string `json:"want"`          // what setting it again sets
	Detail string `json:"detail"`        // what exactly is off, for a tooltip
	// Addr is where an unreachable agent is pointed; Move, when set, the
	// address WSL reaches Windows at now, which Reapply points it at.
	Addr string `json:"addr,omitempty"`
	Move string `json:"move,omitempty"`
}

// started is when this process — and the gateway in it — came up: before
// then, a request the gateway missed says nothing about the agent.
var started = time.Now()

// Drift says what, if anything, keeps the agent off what uni-switch set: its
// config first (wiring, then each field against uni-switch's record), then —
// the config being right — whether its latest use actually came through.
// A field moved from one uni-switch model to another (the agent's own picker)
// isn't drift; one moved off uni-switch is.
func (a *Agent) Drift() *Drift {
	if a.Native != nil {
		s := a.Native.Read()
		if s.Provider == "invalid" {
			return &Drift{Kind: "unwired", Field: "model", Detail: s.Detail}
		}
		return nil
	}
	if len(a.Fields) == 0 || a.theInstalled() {
		return nil
	}
	vals := a.Values()
	opts := fieldOptions{}
	// the field on one of uni-switch's models: where drift shows, and what
	// setting it again sets
	on, onUniSwitch := a.Fields[0], false
	for _, f := range a.Fields {
		if uniSwitchValueIn(a, f, vals[f.Key], vals, opts) {
			on, onUniSwitch = f, true
			break
		}
	}
	// a provider named uni-switch the user wrote by hand is theirs to keep as
	// they like: there is nothing uni-switch set to drift from, and Reapply
	// would write uni-switch's over it
	if onUniSwitch && a.handWired() {
		return nil
	}
	if a.Check != nil {
		if d := a.Check(); d != "" {
			return &Drift{Kind: "unwired", Field: on.Key, Now: vals[on.Key], Want: vals[on.Key], Detail: d}
		}
	}
	rec := appliedOf(a.ID)
	// still joined, a model off uni-switch is one picked in the agent, beside
	// uni-switch's in its list (dsh's /model), not a change from outside; so is
	// one Codex picks beside its ChatGPT sign-in, which still goes through
	// uni-switch (#2424: the row went red with Reconnect)
	joined := a.Joined != nil && a.Joined()
	picked := joined || a.Beside != nil && a.Beside()
	for _, f := range a.Fields {
		want, ok := rec.Fields[f.Key]
		// a field that reads empty while it follows another (Claude Code's
		// tiers and subagents on its main model) runs on that one's model:
		// the main model moved onto the one uni-switch set the field to reads
		// as following it, not as the field put back to the agent's own
		// default (#1050)
		now := vals[f.Key]
		if now == "" && f.Follows != "" {
			now = vals[f.Follows]
		}
		if !ok || picked || now == want || !uniSwitchValueIn(a, f, want, vals, opts) || uniSwitchValueIn(a, f, now, vals, opts) {
			continue
		}
		return &Drift{Kind: "replaced", Field: f.Key, Now: vals[f.Key], Want: want,
			Detail: a.Name + "'s config was changed outside uni-switch: " + f.Label + " is " + orDefault(vals[f.Key]) + ", not " + want + " as uni-switch set it"}
	}
	if onUniSwitch || joined {
		if d := a.unreachable(on, vals[on.Key]); d != nil {
			return d
		}
		if d := a.proxied(on, vals[on.Key]); d != nil {
			return d
		}
	}
	if a.Reached != nil && onUniSwitch {
		if at, to, refused := a.Reached(rec.At); !at.IsZero() {
			switch {
			// a request the gateway saw since, or one to where the config
			// keeps uni-switch (Codex's kept address, a reverse proxy, #1985),
			// came through
			case !sameHost(to, gateway.URL()) && !sameHost(to, a.pointedAt()) && lastSeen(a.ID).Before(at.Add(-2*time.Second)):
				return &Drift{Kind: "bypassed", Field: on.Key, Now: vals[on.Key], Want: vals[on.Key],
					Detail: a.Name + "'s last request (" + at.Format("15:04") + ") went to " + hostOf(to) + ", not uni-switch — it was started before uni-switch set it up and still runs on its old config: quit and reopen it"}
			case refused:
				return &Drift{Kind: "bypassed", Field: on.Key, Now: vals[on.Key], Want: vals[on.Key],
					Detail: a.Name + " couldn't reach uni-switch at " + at.Format("15:04") + " — uni-switch wasn't running then, so it has none of uni-switch's models: quit and reopen it"}
			}
		}
	}
	if a.LastUsed != nil && onUniSwitch {
		if used := a.LastUsed(); bypassed(used, rec.At, lastSeen(a.ID)) {
			return &Drift{Kind: "bypassed", Field: on.Key, Now: vals[on.Key], Want: vals[on.Key],
				Detail: a.Name + " was used at " + used.Format("15:04") + " but none of its requests reached uni-switch — one started before uni-switch set it up still runs on its old config: restart it"}
		}
	}
	return nil
}

// installedURL is the gateway of the uni-switch people install, on its own
// port, the one its Settings have; a uni-switch UNI_SWITCH_ADDR puts on another
// (uni-switch-dev, a sandbox) is run beside it.
var installedURL = gateway.SavedURL

// elsewhere: this uni-switch is not on the installed one's port.
func elsewhere() bool { return !sameHost(gateway.URL(), installedURL()) }

// theInstalled: this uni-switch runs beside the installed one, and the agent is
// wired to that one's gateway, not this one's — the installed uni-switch set it
// up, so it is that one's to check, not drift here. Otherwise the two would
// take turns flagging each other's wiring.
func (a *Agent) theInstalled() bool {
	if !elsewhere() || a.Path == "" {
		return false
	}
	// the bytes filememo keeps, looked at in place: a copy of the file and
	// two strings of it for each field's drift was 5 MB a poll of a Codex
	// config.toml of 1.5 MB, in a uni-switch beside the installed one (a dev
	// build)
	b, err := filememo.Read("bytes", a.Path, func(b []byte) ([]byte, error) { return b, nil })
	if err != nil {
		return false
	}
	return bytes.Contains(b, []byte(hostOf(installedURL()))) && !bytes.Contains(b, []byte(hostOf(gateway.URL())))
}

// bypassed: the agent was used — while this gateway was up and after uni-switch
// last set it — and no request of it arrived since. A request leaves within
// moments of the prompt; a little grace keeps one in flight from counting.
// lastSeen is when the agent's last request reached this process's gateway
// (usage.LastSeen). What usage.Saw records lasts as long as the process, so
// a test that stands for a request gives its own here.
var lastSeen = usage.LastSeen

func bypassed(used, applied, seen time.Time) bool {
	const grace = 30 * time.Second
	return !used.IsZero() && used.After(started) && used.After(applied) &&
		time.Since(used) > grace && seen.Before(used.Add(-2*time.Second))
}

// uniSwitchValue: the value is one of uni-switch's models as this agent spells it,
// its suffix after the model (SplitSuffix) aside; a list of models is the
// user's own.
func uniSwitchValue(a *Agent, f Field, v string, vals map[string]string) bool {
	return uniSwitchValueIn(a, f, v, vals, nil)
}

// fieldOptions keeps each field's options, by its key, for one look at an
// agent's values: Drift asks uniSwitchValue up to three times a field, and
// Claude Code's options (claudeViauni-switch, its own models) were made anew
// each time, 35 MB of what forty polls with real accounts allocated.
type fieldOptions map[string][]Option

func (o fieldOptions) of(f Field, vals map[string]string) []Option {
	if o == nil {
		return f.Options(vals)
	}
	opts, ok := o[f.Key]
	if !ok {
		opts = f.Options(vals)
		o[f.Key] = opts
	}
	return opts
}

// uniSwitchValueIn is uniSwitchValue reading a field's options from opts, made
// for the same vals; nil reads them anew.
func uniSwitchValueIn(a *Agent, f Field, v string, vals map[string]string, opts fieldOptions) bool {
	if v == "" {
		return false
	}
	v, _, one := a.split(v)
	if !one || a.Spelled != nil && !a.Spelled(v) {
		return false
	}
	if isUniSwitch(v) {
		return true
	}
	// spelled as uni-switch's provider in the agent's own config (OpenCode's
	// uni-switch/…): uni-switch's even when its list there doesn't have the model
	if ref, ok := strings.CutPrefix(v, uniSwitchID+"/"); ok && isUniSwitch(ref) {
		return true
	}
	if f.Options == nil {
		return false
	}
	for _, o := range opts.of(f, vals) {
		if o.Value == v {
			return o.Ref != ""
		}
	}
	return false
}

func orDefault(v string) string {
	if v == "" {
		return "the agent's default"
	}
	return v
}

// Reapply sets again what uni-switch set on the agent: what drifted, else its
// fields as they read now — for a config taken off uni-switch in a way no check
// catches. A replaced field brings back the others uni-switch set with it.
// Either way the record is renewed, so a use before now no longer counts.
func (a *Agent) Reapply() error {
	if a.Native != nil {
		return a.Native.Connect()
	}
	// An adapter with an active-routing predicate can read as disconnected
	// after another tool changed its gateway. Reconnect is an explicit user
	// action, so restore its provider even though the current value is empty.
	if a.WiredIn != nil && !a.Wired() {
		for _, f := range a.Fields {
			for _, o := range f.Options(a.Values()) {
				if o.Value == uniSwitchID {
					return a.Apply(f.Key, uniSwitchID)
				}
			}
		}
	}
	d := a.Drift()
	if d != nil && d.Kind == "unreachable" && d.Move != "" && a.move != nil {
		return a.move(d.Addr, d.Move)
	}
	if d != nil && d.Kind == "replaced" {
		rec := appliedOf(a.ID)
		// the model first: the others (an effort) are checked against it
		if err := a.Apply(d.Field, d.Want); err != nil {
			return err
		}
		for _, f := range a.Fields {
			if v, ok := rec.Fields[f.Key]; ok && f.Key != d.Field && f.Get() != v {
				if err := a.Apply(f.Key, v); err != nil {
					return err
				}
			}
		}
		return nil
	}
	vals := a.Values()
	for _, f := range a.Fields {
		// uni-switch as the agent's provider too (Claude Desktop's provider
		// field, beside its tiers on uni-switch's models), as Wired takes it:
		// left out, Reconnect set the tiers again and never the gateway
		// Check found off (dumplings on Discord)
		if v := vals[f.Key]; v != "" && (d != nil && f.Key == d.Field || v == uniSwitchID || uniSwitchValue(a, f, v, vals)) {
			if err := a.Apply(f.Key, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// Keep takes the agent's config as it is now: what uni-switch set before is
// forgotten, and no longer said to have been changed. The agent stays one
// uni-switch set up (setUp): an entry with no fields is kept for it, so one
// whose uni-switch is spelled as a provider of its own config still reads as
// connected.
func (a *Agent) Keep() {
	appliedMu.Lock()
	defer appliedMu.Unlock()
	m := appliedLoad()
	if _, ok := m[a.ID]; !ok {
		return
	}
	m[a.ID] = applied{Kept: true}
	appliedSave(m)
}

// forgetSetUp is Disconnect's Keep: what uni-switch set is forgotten, and so is
// that uni-switch set the agent up at all.
func (a *Agent) forgetSetUp() {
	appliedMu.Lock()
	defer appliedMu.Unlock()
	m := appliedLoad()
	if _, ok := m[a.ID]; !ok {
		return
	}
	delete(m, a.ID)
	appliedSave(m)
}

// setUp reports whether uni-switch put the agent on uni-switch and it hasn't been
// disconnected since: a Connect, which stashes what the agent was on
// (connect.was, since 10-03, which a Keep never took away), or a field
// uni-switch set to one of its models or to itself (applied.json, which Keep
// keeps an entry in). A field set in uni-switch to a model of the agent's own,
// or its thinking level, doesn't set it up.
func (a *Agent) setUp() bool {
	if stashShared()[a.ID+".connect.was"] != "" {
		return true
	}
	rec := appliedOf(a.ID)
	if rec.Kept {
		return true
	}
	for k, v := range rec.Fields {
		// uni-switch/<model> as uni-switch wrote it, its model since gone from the
		// catalog too
		if f := a.Field(k); f != nil && (v == uniSwitchID || prefixed(v) || uniSwitchValue(a, *f, v, rec.Fields)) {
			return true
		}
	}
	return false
}

// handWired: uni-switch is spelled as a provider of the agent's own config
// (Spelled: Pi's uni-switch/<model>, OpenCode's provider at the gateway), and
// uni-switch never set the agent up. A provider named uni-switch that the user
// wrote by hand is theirs (JFunfunfun on X): the agent isn't connected,
// and uni-switch neither syncs its list there, renames in it, nor takes it out
// on Disconnect.
//
// An app connected natively (Aside) keeps ownership its own way: its
// provider is uni-switch's by the key uni-switch wrote in it.
func (a *Agent) handWired() bool { return a.Native == nil && a.Spelled != nil && !a.setUp() }

// Wired reports whether uni-switch is in the agent's config: a field on one of
// uni-switch's models, or on uni-switch itself (an app whose one setting is uni-switch
// as its provider). A model on a provider of the agent's own config named
// uni-switch counts only where uni-switch set the agent up (handWired).
func (a *Agent) Wired() bool {
	if a.Native != nil {
		return a.Native.Read().Provider == "connected"
	}
	if a.WiredIn != nil {
		return a.WiredIn()
	}
	// joined by a provider named uni-switch in the agent's own config (Hermes's
	// providers.uni-switch, VS Code's uni-switch group), as a model on it is
	if a.Joined != nil && a.Joined() || a.Beside != nil && a.Beside() {
		return !a.handWired()
	}
	vals := a.Values()
	for _, f := range a.Fields {
		if v := vals[f.Key]; v == uniSwitchID || uniSwitchValue(a, f, v, vals) {
			return !a.handWired()
		}
	}
	return false
}

// Pick sets a field from the Agents page's model picker. One of uni-switch's
// models picked on an agent not connected yet connects it first, as its
// switch does, then starts it on that model (the owner: the switch, and a
// model picked in uni-switch in one click, both): the agent gets uni-switch's whole
// list, and Disconnect puts back what it had before.
func (a *Agent) Pick(key, v string) error {
	if a.Native != nil {
		value, err := a.Spell(key, v)
		if err != nil {
			return err
		}
		return a.Apply(key, value)
	}
	if f := a.Field(key); f != nil && !a.Wired() {
		vals := a.Values()
		vals[key] = v
		if v == uniSwitchID || uniSwitchValue(a, *f, v, vals) {
			if _, err := a.connect(); err != nil {
				return err
			}
		}
	}
	return a.Apply(key, v)
}

// Connect puts uni-switch into the agent's config, the Agents page's switch
// (the owner: an agent is connected to uni-switch, and its models are picked in
// the agent): the agent's main field goes to one of uni-switch's models — the
// one serving the model it is on now, else the first — or to uni-switch itself
// for an app whose one setting is uni-switch as its provider. Setting it writes
// the provider and the whole catalog in, so the agent's own model list has
// every one of uni-switch's models. An agent that can keep the model it is on
// (Join) keeps it. An agent already connected is left as it is.
// NoModelsError is Connect's answer while uni-switch has no models to give
// the agent: no provider or subscription has been added yet. The GUI says
// it in the reader's language by its code (no_models).
type NoModelsError struct{ Agent string }

func (e *NoModelsError) Error() string {
	return "Add a provider or subscription in uni-switch first, then connect " + e.Agent
}

// AllHiddenError is Connect's answer when uni-switch has models but none of
// them is left in the agent's lists: the only ones picked for it are gone
// with their provider, or every one is turned off for it (#1989). Saying
// it can't be connected sent the user looking at the agent, when its model
// list is what emptied it. The GUI says it in the reader's language by its
// code (all_hidden).
type AllHiddenError struct{ Agent string }

func (e *AllHiddenError) Error() string {
	return "None of uni-switch's models is left in " + e.Agent + "'s model list: pick one under Model list → Pick on the Agents page, then connect " + e.Agent
}

func (a *Agent) Connect() error {
	_, err := a.ConnectHow()
	return err
}

// Connection is how Connect chose the model an agent starts on, for the
// Agents page to say it (#726: Codex showed its last pick, Claude Code a
// Sonnet through uni-switch and Antigravity some model, with nothing saying
// why). How is one of:
//
//   - "kept": it was connected already, left as it was
//   - "again": the models it was on when it was switched off, put back
//   - "joined": the model it is on stays its own (Codex signed in with
//     ChatGPT), uni-switch's models join its list
//   - "uni-switch": an app whose one setting is uni-switch as its provider
//   - "same": the model it is on now, through uni-switch
//   - "alike": the model its alias (opus) stands for, through uni-switch
//   - "default": on its own default with no model set: the first of its
//     own models in its list (the newest, for Claude Code), through uni-switch
//   - "first": uni-switch has none of the model it is on: the first on the
//     account it is signed in to, else a subscription's, else the first
//
// Field and Value are the field set and what it was set to, where Connect
// picked one model ("" for kept, again and joined).
type Connection struct {
	How   string `json:"how"`
	Field string `json:"field,omitempty"`
	Value string `json:"value,omitempty"`
}

// Rewired, when set, is told of an agent that Connect or Disconnect
// changed: the library gives Codex its servers another way on uni-switch.
var Rewired func(id string)

func (a *Agent) rewired(err error) {
	if err == nil && Rewired != nil {
		Rewired(a.ID)
	}
}

// ConnectHow is Connect, saying how the model was chosen.
func (a *Agent) ConnectHow() (c Connection, err error) {
	defer func() { a.rewired(err) }()
	if a.Native != nil {
		return Connection{How: "joined"}, a.Native.Connect()
	}
	if len(a.Fields) == 0 || a.Wired() {
		return Connection{How: "kept"}, nil
	}
	return a.connect()
}

// disconnectedKey, after an agent's id, marks in the stash an agent the
// user disconnected: Disconnect sets it, connecting again takes it out.
const disconnectedKey = ".disconnected"

// Disconnected reports whether the user disconnected the agent and hasn't
// connected it since.
func (a *Agent) Disconnected() bool { return stashShared()[a.ID+disconnectedKey] == "1" }

// noModelLeft says why there is no model of uni-switch's to connect the agent
// to: none added yet (NoModelsError), or every one taken out of its lists
// (AllHiddenError); nil while one is left.
func (a *Agent) noModelLeft() error {
	shown, hidden := provider.CatalogFor(a.ListsFor())
	// nothing to connect it to yet: no provider or subscription added,
	// which said only that it can't be connected (Tystem on Discord)
	if len(shown)+len(hidden) == 0 {
		return &NoModelsError{Agent: a.Name}
	}
	// uni-switch has models, and every one of them is taken out of the
	// agent's lists (a picked model gone with its provider, one hidden by
	// hand): saying the agent can't be connected sends the user looking
	// at the agent, when its model list is what emptied it (#1989)
	if len(shown) == 0 {
		return &AllHiddenError{Agent: a.Name}
	}
	return nil
}

func (a *Agent) connect() (Connection, error) {
	forget(a.ID + disconnectedKey)
	// what it is on now, for Disconnect to put back what it can't
	// otherwise (Goose's own model)
	now := a.Values()
	b, _ := json.Marshal(now)
	stash(map[string]string{a.ID + ".connect.was": string(b)})
	// switched off and on again: the models it was on, as they were
	if ok, err := a.reconnect(now); ok || err != nil {
		return Connection{How: "again"}, err
	}
	// the model it is on stays, where the agent can have uni-switch's models
	// beside it (the owner: Codex keeps its own last pick)
	if a.Join != nil {
		if ok, err := a.Join(); ok || err != nil {
			return Connection{How: "joined"}, err
		}
	}
	// the field uni-switch is picked in: the one listing uni-switch's models (for
	// Gemini CLI its model, its first field being how it signs in), else one
	// whose value is uni-switch itself
	vals := a.Values()
	var f *Field
	var opts []Option
	for pass := 0; pass < 2 && f == nil; pass++ {
		for i := range a.Fields {
			if a.Fields[i].Options == nil {
				continue
			}
			list := a.Fields[i].Options(vals)
			for _, o := range list {
				if pass == 0 && o.Ref != "" || pass == 1 && o.Value == uniSwitchID {
					f, opts = &a.Fields[i], list
					break
				}
			}
			if f != nil {
				break
			}
		}
	}
	if f == nil {
		if err := a.noModelLeft(); err != nil {
			return Connection{}, err
		}
		return Connection{}, fmt.Errorf("%s can't be connected to uni-switch", a.Name)
	}
	cur := connectWas(vals[f.Key], opts)
	if cur == "" && f.Key != a.Fields[0].Key {
		cur = connectWas(a.Values()[a.Fields[0].Key], nil)
	}
	onDefault := cur == ""
	if cur == "" {
		// on its default: the first of its own models
		for _, o := range opts {
			if o.Ref == "" && o.Group == a.Name && o.Value != uniSwitchID {
				cur = connectWas(o.Value, opts)
				break
			}
		}
	}
	// the model it is on now, through uni-switch (or of its family, for an
	// alias): on the account it is signed in to (its vendor's) first, then
	// on a subscription, before a key's; else its own vendor's; else a
	// subscription's; else the first
	var pick, same, alike, own, sub, group string
	sameRank, alikeRank := -1, -1
	for _, o := range opts {
		if o.Value == uniSwitchID {
			if err := a.Apply(f.Key, uniSwitchID); err != nil {
				return Connection{}, err
			}
			// an app whose picker gets uni-switch's models as listed for it
			// (WorkBuddy, ZCode): with none listed, setting uni-switch wrote
			// none, and the agent reads as not connected right after a
			// connect that said it was (Lumens on Discord). One connected
			// by its address alone (Cursor Private Inference) is connected
			// all the same.
			if !a.Wired() {
				if err := a.noModelLeft(); err != nil {
					return Connection{}, err
				}
			}
			return Connection{How: "uni-switch", Field: f.Key, Value: uniSwitchID}, nil
		}
		if o.Ref != "" && o.Group == RoutingGroups && group == "" {
			group = o.Value
		}
		if o.Ref == "" || o.Group == RoutingGroups {
			continue
		}
		rank := 0
		if o.own || o.Group == a.Name {
			rank = 2
		} else if o.sub {
			rank = 1
		}
		if pick == "" {
			pick = o.Value
		}
		id := o.Ref[strings.LastIndex(o.Ref, "/")+1:]
		if cur != "" && (id == cur || strings.TrimSuffix(id, "[1m]") == cur) && rank > sameRank {
			same, sameRank = o.Value, rank
		}
		// an alias (opus) no label read: the model of that family
		if cur != "" && !strings.Contains(cur, "-") && strings.Contains(id+"-", "-"+cur+"-") && rank > alikeRank {
			alike, alikeRank = o.Value, rank
		}
		if own == "" && (o.own || o.Group == a.Name) {
			own = o.Value
		}
		if sub == "" && o.sub {
			sub = o.Value
		}
	}
	// the model it is on, of its own, as uni-switch serves it on a sign-in of
	// the user's, which its field lists as its own (Codex's on a ChatGPT
	// account it can't join, #940)
	if same == "" && alike == "" && cur != "" && a.OwnVia != nil {
		same = a.OwnVia(cur)
	}
	how := "first"
	switch {
	case same != "":
		pick, how = same, "same"
		if onDefault {
			how = "default"
		}
	case alike != "":
		pick, how = alike, "alike"
	case own != "":
		pick = own
	case sub != "":
		pick = sub
	case pick == "" && group != "":
		// only routing groups are shown it (its models hidden, or its own
		// account's alone beside them): the first group (#939)
		pick = group
	}
	if pick == "" {
		return Connection{}, fmt.Errorf("uni-switch has no models %s can use: add a subscription or a provider first", a.Name)
	}
	return Connection{How: how, Field: f.Key, Value: pick}, a.Apply(f.Key, pick)
}

// connectWas is the model an agent is on before it is connected, as a
// model id: its own option's alias read through the label that names the
// model ("opus · claude-opus-5-5"), a provider's prefix taken off.
func connectWas(v string, opts []Option) string {
	for _, o := range opts {
		if o.Value == v && o.Ref == "" {
			if _, id, ok := strings.Cut(o.Label, " · "); ok {
				v = strings.TrimSpace(id)
			}
			break
		}
	}
	if i := strings.LastIndex(v, "/"); i >= 0 {
		v = v[i+1:]
	}
	return strings.TrimSpace(v)
}

// Disconnect takes uni-switch out of the agent's config and puts back what the
// user had, the Agents page's "Disconnect from uni-switch" (Fate on Discord:
// picking the agent's default did it, but nothing said so). Unwire first,
// for an agent whose default would leave it as installed; then each field
// still on uni-switch goes to its default, as picking it does, and so does
// each one uni-switch set that reads as uni-switch set it (an effort). What uni-switch
// remembered setting is forgotten.
func (a *Agent) Disconnect() (err error) {
	defer func() { a.rewired(err) }()
	if a.Native != nil {
		plan, err := a.Native.Disconnect()
		if err != nil {
			return err
		}
		return a.Native.Execute(plan)
	}
	if !a.Wired() {
		return nil
	}
	// the user's say, kept till they connect it again: nothing uni-switch does
	// by itself (a start's Sync, Codex's account failover) puts uni-switch back
	// in, this Disconnect's own writes included (paynezhuang on Discord)
	stash(map[string]string{a.ID + disconnectedKey: "1"})
	// the user's own say over a harness Kiro Crew's connect put on
	kiroCrewUserLeft(a.ID)
	before, rec := a.Values(), appliedOf(a.ID)
	if a.Unwire != nil {
		if err := a.Unwire(); err != nil {
			return err
		}
	}
	for _, f := range a.Fields {
		vals := a.Values()
		v := vals[f.Key]
		// set by uni-switch: as it set it, or its group by the model's name
		set := v != "" && rec.Fields[f.Key] == v && before[f.Key] == v
		if v == "" || !set && v != uniSwitchID && !uniSwitchValue(a, f, v, vals) {
			continue
		}
		if err := f.Set(""); err != nil {
			return fmt.Errorf("%s: %w", f.Label, err)
		}
	}
	// a field its own Set left empty goes back to what it was on before
	// Connect (Goose's own provider and model)
	var was map[string]string
	json.Unmarshal([]byte(unstash(a.ID+".connect.was")), &was)
	after := a.Values()
	// connected while on a provider named uni-switch it had by hand: what it
	// was on then is uni-switch's route, its thinking level read off it too
	// (dsh's), not the agent's own to put back
	for _, f := range a.Fields {
		if w := was[f.Key]; w == uniSwitchID || uniSwitchValue(a, f, w, was) {
			was = nil
			break
		}
	}
	for _, f := range a.Fields {
		if w := was[f.Key]; w != "" && after[f.Key] == "" && w != uniSwitchID && !uniSwitchValue(a, f, w, was) {
			if err := f.Set(w); err != nil {
				return fmt.Errorf("%s: %w", f.Label, err)
			}
		}
	}
	if a.Restore != nil {
		if err := a.Restore(); err != nil {
			return err
		}
	}
	// what it was on through uni-switch, for switching it on again
	back := reconnection{UniSwitch: map[string]string{}, Left: a.Values()}
	for _, f := range a.Fields {
		if v := before[f.Key]; v != back.Left[f.Key] {
			back.UniSwitch[f.Key] = v
			if v == uniSwitchID || uniSwitchValue(a, f, v, before) {
				back.Routed = append(back.Routed, f.Key)
			}
		}
	}
	b, _ := json.Marshal(back)
	stash(map[string]string{a.ID + ".reconnect": string(b)})
	a.forgetSetUp()
	return nil
}

// DisconnectOffline restores a native agent's saved files only after an
// explicit offline choice. Legacy callers of Disconnect keep live semantics.
func (a *Agent) DisconnectOffline() error {
	if a.Native == nil || a.Native.ExecuteOffline == nil {
		return fmt.Errorf("%s does not support offline disconnect", a.Name)
	}
	plan, err := a.Native.Disconnect()
	if err != nil {
		return err
	}
	return a.Native.ExecuteOffline(plan)
}

// reconnection is what Disconnect took an agent off: the values of its
// fields it changed (Routed: those that were uni-switch's) and what it left
// every field at.
type reconnection struct {
	UniSwitch map[string]string `json:"uni-switch"`
	Routed    []string          `json:"routed,omitempty"`
	Left      map[string]string `json:"left"`
}

// reconnect puts back the models an agent was on when it was disconnected
// (the owner: an agent remembers its last pick), if nothing has changed
// it since and uni-switch still routes them; false when it doesn't apply.
func (a *Agent) reconnect(now map[string]string) (bool, error) {
	var rec reconnection
	if json.Unmarshal([]byte(unstash(a.ID+".reconnect")), &rec) != nil || len(rec.Routed) == 0 {
		return false, nil
	}
	for k, v := range rec.Left {
		if now[k] != v {
			return false, nil
		}
	}
	for _, k := range rec.Routed {
		f := a.Field(k)
		if f == nil || rec.UniSwitch[k] != uniSwitchID && !uniSwitchValue(a, *f, rec.UniSwitch[k], rec.UniSwitch) {
			return false, nil
		}
	}
	// joined first, where the agent can be (Codex signed in with ChatGPT),
	// as the switch connects it the first time: its own models stay
	// through uni-switch beside the one put back. Put back on a routing group
	// alone, Codex was beside its sign-in and not joined, so picking one
	// of its own on the Agents page took it off uni-switch, every OpenAI model
	// then said direct, and after a group again they said via uni-switch
	// though a pick still went straight to OpenAI (NancyTom18 on X). One
	// that can't join now (a provider of the user's own, uni-switch API) is
	// connected as at first unless its model was one of uni-switch's, which
	// routes it: its subagents' or memories' model put back alone read as
	// connected while its own model stayed off uni-switch (#3868)
	if a.Join != nil {
		ok, err := a.Join()
		if err != nil {
			return true, err
		}
		if !ok && !slices.Contains(rec.Routed, a.Fields[0].Key) {
			return false, nil
		}
	}
	// the models on uni-switch first, as Connect sets them (setting Gemini
	// CLI's model signs it in through uni-switch, which its sign-in field
	// can't), then the rest that aren't so by then
	var keys []string
	for _, k := range rec.Routed {
		if rec.UniSwitch[k] != uniSwitchID {
			keys = append(keys, k)
		}
	}
	for _, f := range a.Fields {
		if _, ok := rec.UniSwitch[f.Key]; ok && !slices.Contains(keys, f.Key) {
			keys = append(keys, f.Key)
		}
	}
	for _, k := range keys {
		v := rec.UniSwitch[k]
		if v != a.Values()[k] {
			if err := a.Apply(k, v); err != nil {
				return true, err
			}
		} else {
			// set along with the model (Codex's effort): uni-switch's as before,
			// for Disconnect to take out again
			record(a.ID, k, v)
		}
	}
	return a.Wired(), nil
}

// lastJSONLTime reads the newest Unix timestamp (seconds or milliseconds)
// under key in the last lines of a prompt log, passing over the lines whose
// text (under textKey) is a slash command: those an agent answers itself.
// Zero if there is none.
func lastJSONLTime(path, key, textKey string) time.Time {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	const tail = 64 << 10
	if st, err := f.Stat(); err == nil && st.Size() > tail {
		f.Seek(st.Size()-tail, 0)
	}
	var last int64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var m map[string]json.RawMessage
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		var text string
		if json.Unmarshal(m[textKey], &text) == nil && strings.HasPrefix(strings.TrimSpace(text), "/") {
			continue
		}
		n, err := strconv.ParseInt(string(m[key]), 10, 64)
		if err != nil {
			continue
		}
		if n > 1e12 {
			n /= 1000
		}
		last = max(last, n)
	}
	if last == 0 {
		return time.Time{}
	}
	return time.Unix(last, 0)
}

// sameHost: two URLs name the same server, whatever their scheme (Codex
// asks uni-switch's http gateway over ws) or path.
func sameHost(a, b string) bool { return hostOf(a) == hostOf(b) }

// wiringOff checks what uni-switch wrote into an agent's config to reach the
// gateway — pairs of key and value, read with get — and says which no longer
// holds, "" when all do. A URL is quoted; a key's value never is. A URL
// that reaches uni-switch another way (reachesuni-switch: a reverse proxy, #1985)
// holds. So does a key of the gateway's own the user put in place of
// uni-switch's (keyHolds).
func wiringOff(name, file string, get func(string) (string, bool), kvs ...string) string {
	for i := 0; i+1 < len(kvs); i += 2 {
		k, want := kvs[i], kvs[i+1]
		v, _ := get(k)
		if keyHolds(v, want) || v != "" && reachesUniSwitch(v, want) {
			continue
		}
		return whatsOff(name, file, k, v, want)
	}
	return ""
}

// whatsOff is what wiringOff says of k in an agent's config, v where
// uni-switch wrote want.
func whatsOff(name, file, k, v, want string) string {
	where := name + "'s " + k + " (" + filepath.Base(file) + ")"
	switch {
	case v == "":
		return where + " is gone, so it no longer reaches uni-switch"
	case strings.Contains(strings.ToLower(k), "url"):
		return where + " is " + v + ", not uni-switch's gateway at " + want
	default:
		return where + " was changed, so uni-switch's gateway won't take its requests"
	}
}
