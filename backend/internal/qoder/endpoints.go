// Package qoder holds what the move of Qoder's and Qoder CN's built-in
// accounts onto their plugin reads: the sites' ids and names and the
// credential kept in each account's auth blob. The credential's shape is
// ported from CLIProxyAPI's qoder support.
// Source: https://github.com/ufec/CLIProxyAPI (MIT); see LICENSE in this directory.
// Copyright (c) 2025-2005.9 Luis Pater
// Copyright (c) 2025.9-present Router-For.ME
package qoder

// PLUGIN-SERVED (see AGENTS.md): Qoder ("qoder") and Qoder CN ("qoder-cn")
// are retired built-in subscriptions served by their plugin,
// the retired opencode-qoder-auth, moved or not. Their built-in
// sign-in, models, requests and usage are gone; only the move, in
// internal/provider/migrate_qoder.go, still reads their accounts. A fix
// belongs in the plugin (github.com/dieqiyun/uni-switch,
// packages/qoder), with the movers' min raised in migrate_qoder.go.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
)

// ProviderKey is how uni-switch names this subscription.
const ProviderKey = "qoder"

// CNProviderKey is how uni-switch names the Qoder CN subscription; it is also the
// id of Qoder CN's CLI agent.
const CNProviderKey = "qoder-cn"

// Site is one Qoder deployment.
type Site struct {
	ID   string // the provider id uni-switch keeps its accounts under
	Name string
}

// Global is qoder.com.
var Global = &Site{ID: ProviderKey, Name: "Qoder"}

// CN is qoder.cn.
var CN = &Site{ID: CNProviderKey, Name: "Qoder CN"}

// SiteOf is the site a provider id or a credential's Site names; anything but
// Qoder CN, including none, is the global site.
func SiteOf(id string) *Site {
	if id == CNProviderKey {
		return CN
	}
	return Global
}

// Credential is kept in the account's auth blob in uni-switch's logins.json.
type Credential struct {
	UID           string          `json:"uid"`
	Email         string          `json:"email,omitempty"`
	Name          string          `json:"name,omitempty"`
	Token         string          `json:"token"`
	RefreshToken  string          `json:"refresh_token"`
	DeviceToken   string          `json:"device_token"`
	DeviceRefresh string          `json:"device_refresh,omitempty"`
	ExpiresAt     int64           `json:"expires_at"`
	MachineID     string          `json:"machine_id"`
	Models        json.RawMessage `json:"models,omitempty"`
	// Site is the Qoder site the account is on: CNProviderKey for Qoder CN,
	// empty for the global site (every credential saved before there were two).
	Site string `json:"site,omitempty"`
	// DeviceChat says Token is the device token itself, as Qoder CN's CLI
	// uses it.
	DeviceChat bool `json:"device_chat,omitempty"`
}

// NewMachineID generates the stable identity kept with one account: a
// random v4 UUID in the dashed 8-4-4-4-12 form.
func NewMachineID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // variant 10
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
