package gateway

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Keep generated history IDs deterministic so adding a turn does not invalidate
// the upstream's prompt cache. Only calls lacking IDs enter the result FIFO.
func repairChatHistory(body Object) {
	var missing []string
	for messageIndex, value := range list(body["messages"]) {
		message := obj(value)
		switch message["role"] {
		case "assistant":
			calls := list(message["tool_calls"])
			if len(calls) == 0 {
				continue
			}
			missing = nil
			for callIndex, value := range calls {
				call := obj(value)
				if str(call["id"]) != "" || call == nil {
					continue
				}
				fn := obj(call["function"])
				position := strconv.Itoa(messageIndex) + "." + strconv.Itoa(callIndex)
				arguments := ""
				if value, exists := fn["arguments"]; exists {
					arguments = encode(value)
				}
				hash := sha256.Sum256([]byte(position + "\x00" + str(fn["name"]) + "\x00" + arguments))
				id := "call_" + hex.EncodeToString(hash[:12])
				call["id"] = id
				missing = append(missing, id)
			}
		case "tool":
			if str(message["tool_call_id"]) == "" && len(missing) > 0 {
				message["tool_call_id"] = missing[0]
				missing = missing[1:]
			}
		}
	}
}

const repeatMark = "usr_repeat1_"

// Messages clients retain tool_use IDs for the whole session. A reused vendor
// ID must be salted for the client, then restored on both history and results.
type messageIDs struct{ seen map[string]bool }

func newMessageIDs(body Object) *messageIDs {
	ids := &messageIDs{seen: map[string]bool{}}
	for _, message := range list(body["messages"]) {
		for _, value := range list(field(message, "content")) {
			block := obj(value)
			if block["type"] == "tool_use" && str(block["id"]) != "" {
				ids.seen[str(block["id"])] = true
			}
		}
	}
	return ids
}

func repeatedID(id string) (string, bool) {
	encoded, ok := strings.CutPrefix(id, repeatMark)
	if !ok {
		return "", false
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || !utf8.Valid(data) || !safeChars(id) {
		return "", false
	}
	original, salt, ok := strings.Cut(string(data), "\x00")
	return original, ok && original != "" && salt != ""
}

func (ids *messageIDs) next(id string) string {
	if ids.seen[id] {
		id = repeatMark + base64.RawURLEncoding.EncodeToString([]byte(rawID(id)+"\x00"+uid("")))
	}
	ids.seen[id] = true
	return id
}

func messageCallID(p Part) string {
	if p.ClientID != "" {
		return p.ClientID
	}
	return safeID(p.ID)
}

func (ids *messageIDs) result(r *Result) {
	for i, p := range r.Parts {
		if p.Kind == "call" {
			r.Parts[i].ClientID = ids.next(safeID(p.ID))
		}
	}
}

func (ids *messageIDs) stream(emit func(Event) error) func(Event) error {
	assigned := map[int]string{}
	return func(e Event) error {
		if e.Kind == "part" && e.Part.Kind == "call" {
			assigned[e.Index] = ids.next(safeID(e.Part.ID))
		}
		if id := assigned[e.Index]; id != "" && e.Part.Kind == "call" {
			e.Part.ClientID = id
		}
		return emit(e)
	}
}

func restoreMessageIDs(body Object) {
	for _, message := range list(body["messages"]) {
		for _, value := range list(field(message, "content")) {
			block := obj(value)
			key := ""
			switch block["type"] {
			case "tool_use":
				key = "id"
			case "tool_result":
				key = "tool_use_id"
			}
			if key != "" {
				block[key] = rawID(str(block[key]))
			}
		}
	}
}
