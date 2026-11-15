package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// While a model is picked for one of Claude Desktop's tiers, uni-switch's
// profile carries inferenceModels (gateway.DesktopInferenceModels): the
// list Desktop's Code tab hands Claude Code each pick's behavesAs from,
// so a pick runs with the prompt of the Claude model of its tier
// (moer2831). Without a pick there is none, and Desktop finds uni-switch's
// models on its own as before. The list is uni-switch's only while it is what
// uni-switch wrote: desktopModelsMark keeps the hash of each list uni-switch
// wrote (the one before a write too, until the write is done), and a list
// with any other hash is the user's, which uni-switch never writes over or
// takes away. Desktop reads its profile at start-up only.
const desktopModelsMark = "claude-desktop.models"

// desktopModelsHash is a list's hash, the same however it is spaced.
func desktopModelsHash(raw []byte) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// desktopSyncModels writes p's inferenceModels as uni-switch lists them now,
// takes away the one uni-switch wrote when nothing is picked, and leaves the
// user's own alone.
func desktopSyncModels(p desktopPaths) error {
	if !desktopSelected(p) {
		return nil
	}
	want, ok := gateway.DesktopInferenceModels()
	if !ok {
		return nil // the picks unknown: what is there stays
	}
	key := desktopModelsMark + p.key
	marks := strings.Fields(stashShared()[key])
	b, err := edit.Read(p.prof)
	if err != nil || b == nil {
		return err // no profile: desktopOn writes one, then this
	}
	raw, has := edit.GetJSONRaw(p.prof, "inferenceModels")
	if has && !slices.Contains(marks, desktopModelsHash([]byte(raw))) {
		return nil // the user's
	}
	if want == nil {
		if has {
			if err := edit.DelJSON(p.prof, "inferenceModels"); err != nil {
				return err
			}
		}
		forget(key)
		return nil
	}
	next, err := json.Marshal(want)
	if err != nil {
		return err
	}
	h := desktopModelsHash(next)
	if has && desktopModelsHash([]byte(raw)) == h {
		stash(map[string]string{key: h})
		return nil
	}
	// marked before it is written: a write cut short leaves a list uni-switch
	// still knows for its own
	stash(map[string]string{key: strings.Join(append(marks, h), " ")})
	if err := edit.SetJSON(p.prof, edit.KV{Path: "inferenceModels", Value: json.RawMessage(next)}); err != nil {
		return err
	}
	stash(map[string]string{key: h})
	return nil
}
