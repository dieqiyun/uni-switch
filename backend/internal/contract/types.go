// Package contract defines the versioned desktop-to-core wire contract.
package contract

import (
	"encoding/json"
	"github.com/dieqiyun/uni-switch/backend/internal/modelmeta"
)

const Version = 1
const MaxRequestBytes = 32 * 1024 * 1024
const MaxResponseBytes = 4 * 1024 * 1024

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *Error) Error() string { return e.Message }

func Fail(code, message string) *Error { return &Error{Code: code, Message: message} }

type Request struct {
	Version int             `json:"version"`
	ID      uint64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type Response struct {
	Version int    `json:"version"`
	ID      uint64 `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Connection struct {
	BaseURL      string        `json:"baseUrl"`
	APIKey       string        `json:"apiKey"`
	Protocol     *string       `json:"protocol"`
	AuthMode     *string       `json:"authMode"`
	Query        *BalanceQuery `json:"query"`
	AccountToken *string       `json:"accountToken"`
}

type Model struct {
	ID                string              `json:"id"`
	ContextWindow     *int64              `json:"contextWindow"`
	ReasoningEfforts  []string            `json:"reasoningEfforts"`
	Enabled           bool                `json:"enabled"`
	Capabilities      map[string]any      `json:"capabilities,omitempty"`
	Profile           map[string]any      `json:"profile,omitempty"`
	MetadataUpdatedAt int64               `json:"metadataUpdatedAt"`
	Metadata          *modelmeta.Resolved `json:"metadata,omitempty"`
}

type Models struct {
	Models   []Model `json:"models"`
	SyncedAt int64   `json:"syncedAt"`
	Protocol string  `json:"protocol"`
	AuthMode string  `json:"authMode"`
	BaseURL  string  `json:"baseUrl"`
}

type BalanceQuery struct {
	Adapter  string  `json:"adapter"`
	SiteURL  *string `json:"siteUrl"`
	UserID   *string `json:"userId"`
	Path     string  `json:"path"`
	JSONPath string  `json:"jsonPath"`
	Unit     string  `json:"unit"`
	Divisor  float64 `json:"divisor"`
}

type Balance struct {
	Amount       *float64 `json:"amount"`
	Unit         string   `json:"unit"`
	CheckedAt    int64    `json:"checkedAt"`
	Scope        string   `json:"scope"`
	Unlimited    bool     `json:"unlimited"`
	Used         *float64 `json:"used"`
	Total        *float64 `json:"total"`
	PlanName     *string  `json:"planName"`
	ExpiresAt    *string  `json:"expiresAt"`
	Windows      []Window `json:"windows"`
	ProviderType *string  `json:"providerType"`
	Note         *string  `json:"note"`
}

type Window struct {
	Label     string  `json:"label"`
	Remaining float64 `json:"remaining"`
	Used      float64 `json:"used"`
	Total     float64 `json:"total"`
	ResetAt   *string `json:"resetAt"`
}
