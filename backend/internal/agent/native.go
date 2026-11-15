package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// NativeConnection registers a provider independently of model selection.
// Its plan describes file and runtime changes without performing either.
type NativeConnection struct {
	Read           func() NativeState
	Connect        func() error
	Apply          func(key, value string) error
	Stage          func(key, value string) error
	Disconnect     func() (*DisconnectPlan, error)
	Execute        func(*DisconnectPlan) error
	ExecuteOffline func(*DisconnectPlan) error
}

type OfflineAction string

const (
	OfflineStage      OfflineAction = "stage"
	OfflineDisconnect OfflineAction = "disconnect"
)

// RuntimeUnavailableError is created only for an initial runtime read,
// before the operation has written either runtime settings or local files.
type RuntimeUnavailableError struct {
	Agent     string
	Operation string
	Offline   OfflineAction
	Cause     error
}

func (e *RuntimeUnavailableError) Error() string {
	return fmt.Sprintf("%s runtime settings could not be read for %s: %v", e.Agent, e.Operation, e.Cause)
}
func (e *RuntimeUnavailableError) Unwrap() error { return e.Cause }

type NativeState struct {
	Provider string                 `json:"provider"`
	Detail   string                 `json:"detail,omitempty"`
	Runtime  string                 `json:"runtime"`
	Fields   map[string]NativeField `json:"fields"`
}

type NativeField struct {
	Value  string `json:"value"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type PlannedFile struct {
	Path   string
	Before []byte
	After  []byte
}

type PlannedSetting struct {
	Key    string
	Before json.RawMessage
	After  json.RawMessage
}

type DisconnectPlan struct {
	Files    []PlannedFile
	Settings []PlannedSetting
}

// Revision binds confirmation to file preconditions and restoration values,
// without accepting a serialized plan from the client.
func (p *DisconnectPlan) Revision(agent string) string {
	b, _ := json.Marshal(struct {
		Agent string
		Plan  *DisconnectPlan
	}{agent, p})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (p *DisconnectPlan) Preview() []FileChange {
	var changes []FileChange
	for _, f := range p.Files {
		lines := diffLines(string(f.Before), string(f.After))
		if len(lines) != 0 {
			path := f.Path
			if home, err := os.UserHomeDir(); err == nil && len(path) > len(home) && path[:len(home)+1] == home+string(os.PathSeparator) {
				path = "~" + path[len(home):]
			}
			changes = append(changes, FileChange{Path: path, Lines: lines})
		}
	}
	return changes
}

func (p *DisconnectPlan) CheckFiles() error {
	for _, f := range p.Files {
		b, err := os.ReadFile(f.Path)
		if os.IsNotExist(err) {
			b, err = nil, nil
		}
		if err != nil {
			return err
		}
		if string(b) != string(f.Before) {
			return fmt.Errorf("%s changed since the disconnect plan was read; try again", f.Path)
		}
	}
	return nil
}
