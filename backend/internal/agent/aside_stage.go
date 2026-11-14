package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// stage is an explicit offline write, separate from confirmed runtime apply.
func (c *asideConnection) stage(key, value string) error {
	if key == "account" {
		return c.pickAccount(value)
	}
	asideMu.Lock()
	defer asideMu.Unlock()
	if key == "effort" || value == "" {
		return fmt.Errorf("select a model to save for Aside's next start")
	}
	value, err := c.validateSelection(key, value)
	if err != nil {
		return err
	}
	p, m, _ := strings.Cut(value, "/")
	b, err := edit.Read(c.path)
	if err != nil {
		return err
	}
	var saved map[string]json.RawMessage
	if len(b) > 0 {
		if err := json.Unmarshal(b, &saved); err != nil {
			return err
		}
	}
	path := asideFieldPath(key)
	before := asideRaw(saved, path)
	if len(before) == 0 {
		before = json.RawMessage("null")
	}
	r, err := c.load()
	if err != nil {
		return err
	}
	owned, had := r.Fields[key]
	if had && !strings.HasPrefix(asideSelection(before), "uni-switch/") {
		delete(r.Fields, key)
		had = false
		owned = asideOwned{}
	}
	if p == "uni-switch" {
		if err := c.connectLocked(); err != nil {
			return err
		}
		if !had && !strings.HasPrefix(asideSelection(before), "uni-switch/") {
			owned.Before = append(json.RawMessage(nil), before...)
		}
	}
	selection := map[string]any{}
	if key != "image" {
		_ = json.Unmarshal(before, &selection)
		if selection == nil {
			selection = map[string]any{}
		}
		if _, ok := selection["thinkingLevel"]; !ok {
			selection["thinkingLevel"] = "medium"
		}
	}
	selection["provider"], selection["modelId"] = p, m
	raw, _ := json.Marshal(selection)
	owned.Pending = append(json.RawMessage(nil), raw...)
	r.Fields[key] = owned
	if err := c.save(r); err != nil {
		return err
	}
	if err := edit.SetJSON(c.path, edit.KV{Path: path, Value: json.RawMessage(raw)}); err != nil {
		return err
	}
	c.read = false
	return nil
}
