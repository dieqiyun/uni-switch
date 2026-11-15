package sessions

import (
	"encoding/json"
	"sort"
)

// openCodeTranscript reads the conversation in message order from the same
// store the listing chose, including while a v1 to v2 migration is underway.
func openCodeTranscript(s Session, add func(bool, Part) bool) error {
	storeMu.RLock()
	defer storeMu.RUnlock()
	// the store handles are shared with the reads that hold mu: they are
	// opened and given back under it, and read from without it
	mu.Lock()
	release := useDBs()
	files := openCodeFiles()
	mu.Unlock()
	defer func() {
		mu.Lock()
		release()
		mu.Unlock()
	}()
	for _, f := range files {
		if f.m().sid != s.ID {
			continue
		}
		if v, ok := f.m().oc.(ocV2DB); ok {
			rows, err := v.db.Query("SELECT type,data FROM session_message WHERE session_id=? AND type IN ('user','assistant') ORDER BY seq", s.ID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var role string
				var raw []byte
				if err := rows.Scan(&role, &raw); err != nil {
					return err
				}
				var m struct {
					Text    string            `json:"text"`
					Content []json.RawMessage `json:"content"`
					Model   struct {
						ID string `json:"id"`
					} `json:"model"`
				}
				if err := json.Unmarshal(raw, &m); err != nil {
					return err
				}
				if role == "user" {
					if !add(true, textPart(role, m.Text)) {
						return nil
					}
					continue
				}
				for _, p := range m.Content {
					if !openCodeTranscriptPart(role, m.Model.ID, p, add) {
						return nil
					}
				}
			}
			return rows.Err()
		}
		var messages []ocMessage
		for _, b := range f.m().oc.messages(f.m().sid) {
			var m ocMessage
			if json.Unmarshal(b, &m) == nil {
				messages = append(messages, m)
			}
		}
		sort.SliceStable(messages, func(i, j int) bool {
			if messages[i].Time.Created != messages[j].Time.Created {
				return messages[i].Time.Created < messages[j].Time.Created
			}
			return messages[i].ID < messages[j].ID
		})
		for _, m := range messages {
			for _, p := range f.m().oc.parts(m.ID) {
				if !openCodeTranscriptPart(m.Role, m.ModelID, p, add) {
					return nil
				}
			}
		}
		return nil
	}
	return ErrNoTranscript
}

// openCodeTranscriptPart tells add one part of a message; model is the
// message's own (an assistant's modelID, or model.id in OpenCode 2), said
// with each of its assistant parts (#2253).
func openCodeTranscriptPart(role, model string, raw []byte, add func(bool, Part) bool) bool {
	say := add
	add = func(input bool, q Part) bool {
		if q.Role == "assistant" {
			q.Model = model
		}
		return say(input, q)
	}
	var p struct {
		Type      string `json:"type"`
		Text      string `json:"text"`
		Name      string `json:"name"`
		Tool      string `json:"tool"`
		Synthetic bool   `json:"synthetic"`
		Ignored   bool   `json:"ignored"`
		State     struct {
			Input   json.RawMessage `json:"input"`
			Output  string          `json:"output"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"state"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Synthetic || p.Ignored {
		return true
	}
	switch p.Type {
	case "text":
		return add(role == "user", textPart(role, p.Text))
	case "reasoning":
		return add(false, Part{Role: role, Kind: "thinking", Text: p.Text})
	case "tool":
		name := p.Name
		if name == "" {
			name = p.Tool
		}
		if !add(false, Part{Role: role, Kind: "tool_use", Name: name, Text: string(p.State.Input)}) {
			return false
		}
		if p.State.Output != "" && !add(false, Part{Role: "tool", Kind: "tool_result", Name: name, Text: p.State.Output}) {
			return false
		}
		for _, r := range p.State.Content {
			if r.Type == "text" && !add(false, Part{Role: "tool", Kind: "tool_result", Name: name, Text: r.Text}) {
				return false
			}
		}
	}
	return true
}
