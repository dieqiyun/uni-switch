package library

import "encoding/json"

// UnmarshalJSON excludes retired automatic remote gateway bridges. Explicitly
// configured MCP servers keep their ordinary command/HTTP behavior.
func (l *Library) UnmarshalJSON(data []byte) error {
	type plain Library
	var servers []json.RawMessage
	view := struct {
		*plain
		MCP *[]json.RawMessage `json:"mcp"`
	}{plain: (*plain)(l), MCP: &servers}
	if err := json.Unmarshal(data, &view); err != nil {
		return err
	}
	l.MCP = nil
	for _, raw := range servers {
		var legacy struct {
			Bridge string `json:"bridge"`
		}
		if err := json.Unmarshal(raw, &legacy); err != nil {
			return err
		}
		if legacy.Bridge != "" {
			continue
		}
		var s Server
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		l.MCP = append(l.MCP, &s)
	}
	return nil
}
