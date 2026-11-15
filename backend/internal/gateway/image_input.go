package gateway

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

const imageOmitted = "[Image omitted: this model accepts text only.]"

// errCurrentImage stops a walk at an image the latest turn gives the model.
var errCurrentImage = errors.New("an image in the latest turn")

// textOnlyBody rejects a user image in the latest turn and omits tool images
// and images in older turns. It edits the wire JSON so passthrough keeps fields
// the IR does not represent (such as provider-specific request options).
func textOnlyBody(proto provider.Protocol, body []byte) ([]byte, bool) {
	out, err := walkImages(proto, body, func(im imageAt) (json.RawMessage, error) {
		if im.Current() {
			return nil, errCurrentImage
		}
		return textBlock(proto, imageOmitted), nil
	})
	if err != nil {
		return body, true
	}
	return out, false
}

// hasImage is whether the body has an image in any turn.
func hasImage(proto provider.Protocol, body []byte) bool {
	found := errors.New("found")
	_, err := walkImages(proto, body, func(imageAt) (json.RawMessage, error) { return nil, found })
	return err == found
}

// hasUnportableCurrentImage reports a current Responses image with no URL to
// carry to another protocol. A file_id belongs to the Responses provider that
// stored it; translating it as a URL or dropping it would mislead the model.
func hasUnportableCurrentImage(body []byte) bool {
	missing := errors.New("current image has no portable source")
	_, err := walkImages(provider.Responses, body, func(im imageAt) (json.RawMessage, error) {
		if im.Current() && im.Src == "" {
			return nil, missing
		}
		return nil, nil
	})
	return err == missing
}

// imageAt is an image in a request, where it is.
type imageAt struct {
	// Src is the image as a URL, a data: URL for one sent inline; empty for
	// one only its vendor can open (a Responses file_id, a Gemini file).
	Src string
	// Last is set on one in the latest turn, Tool on one a tool gave back.
	Last, Tool bool
	// Block is the image's block as it is in the body.
	Block json.RawMessage
}

// Current is whether the image is one the user gives the model now, which
// the model is asked about.
func (im imageAt) Current() bool { return im.Last && !im.Tool }

// walkImages gives every image in the body's turns to swap, and puts the
// block it returns in the image's place (nil keeps it). An error from swap
// ends the walk. The body is returned as it was when nothing was swapped.
func walkImages(proto provider.Protocol, body []byte, swap func(imageAt) (json.RawMessage, error)) ([]byte, error) {
	turnsKey, contentKey := "messages", "content"
	switch proto {
	case provider.Responses:
		turnsKey = "input"
	case provider.Gemini:
		turnsKey, contentKey = "contents", "parts"
	}
	if !anyImageTurn(proto, body, turnsKey, contentKey) {
		// Most requests have no image, and this walk runs several times
		// on each: decoding every turn and block cost ~2.6 MB for each
		// 1.5 MB conversation (yg6888 on X), and the request's top level
		// alone was a copy of it each time (#3725).
		return body, nil
	}
	var request map[string]json.RawMessage
	if decodeJSON(body, &request) != nil {
		return body, nil
	}
	var turns []json.RawMessage
	if decodeJSON(request[turnsKey], &turns) != nil {
		return body, nil
	}
	changed := false
	for i, raw := range turns {
		var turn map[string]json.RawMessage
		if decodeJSON(raw, &turn) != nil {
			continue
		}
		key := contentKey
		tool := false
		var kind string
		if proto == provider.Responses {
			decodeJSON(turn["type"], &kind)
			if kind == "function_call_output" {
				key, tool = "output", true
			}
		}
		if proto == provider.Chat {
			decodeJSON(turn["role"], &kind)
			tool = kind == "tool"
		}
		content, swapped, err := swapImageBlocks(proto, turn[key], i == len(turns)-1, tool, swap)
		if err != nil {
			return body, err
		}
		if !swapped {
			continue
		}
		turn[key] = content
		turns[i], _ = json.Marshal(turn)
		changed = true
	}
	if !changed {
		return body, nil
	}
	request[turnsKey], _ = json.Marshal(turns)
	return json.Marshal(request)
}

// anyImageTurn is whether body may hold a block walkImages gives to swap:
// it is a JSON object whose turnsKey is a list anyImageBlock says yes to,
// read in place. A key given twice is looked at each time.
func anyImageTurn(proto provider.Protocol, body []byte, turnsKey, contentKey string) bool {
	if !jsonValid(body) {
		return false
	}
	raw := trimJSON(body)
	if raw[0] != '{' {
		return false
	}
	found := false
	eachMember(raw, func(key, val []byte) bool {
		if val[0] == '[' && jsonStringIs(key, turnsKey) {
			found = anyImageBlock(proto, val, contentKey)
		}
		return !found
	})
	return found
}

// anyImageBlock is whether turns, a valid JSON array, may hold a block
// walkImages gives to swap, read in place without decoding: a turn's
// contentKey (or a Responses function_call_output's output) that is a list
// with a block among it of an image's type, or one inside an Anthropic
// tool_result's content or a Gemini functionResponse's parts, or a Gemini
// part with inlineData or fileData. It errs toward yes, as for a key
// spelled twice.
func anyImageBlock(proto provider.Protocol, turns []byte, contentKey string) bool {
	found := false
	eachElement(turns, func(turn []byte) bool {
		if turn[0] != '{' {
			return true
		}
		eachMember(turn, func(key, val []byte) bool {
			if val[0] == '[' && (jsonStringIs(key, contentKey) || (proto == provider.Responses && jsonStringIs(key, "output"))) {
				found = anyImageIn(proto, val)
			}
			return !found
		})
		return !found
	})
	return found
}

// anyImageIn is whether blocks, a valid JSON array, has a block
// swapImageBlocks would look into (anyImageBlock).
func anyImageIn(proto provider.Protocol, blocks []byte) bool {
	kind := "image" // Anthropic's
	switch proto {
	case provider.Chat:
		kind = "image_url"
	case provider.Responses:
		kind = "input_image"
	}
	found := false
	eachElement(blocks, func(block []byte) bool {
		if block[0] != '{' {
			return true
		}
		// a tool's images are in its own blocks, looked into as
		// swapImageBlocks does: most agents' turns are tool results,
		// nearly all text (#3725)
		var inner []byte
		toolResult := false
		eachMember(block, func(key, val []byte) bool {
			switch {
			case proto == provider.Gemini && jsonStringIs(key, "functionResponse"):
				if val[0] == '{' {
					eachMember(val, func(k, v []byte) bool {
						if jsonStringIs(k, "parts") {
							inner = v // the last, as a decode keeps
						}
						return true
					})
				}
			case proto == provider.Gemini:
				found = jsonStringIs(key, "inlineData") || jsonStringIs(key, "fileData")
			case jsonStringIs(key, "type"):
				found = jsonStringIs(val, kind)
				toolResult = toolResult || (proto == provider.Anthropic && jsonStringIs(val, "tool_result"))
			case proto == provider.Anthropic && jsonStringIs(key, "content"):
				inner = val
			}
			return !found
		})
		if !found && len(inner) > 0 && inner[0] == '[' && (proto == provider.Gemini || toolResult) {
			found = anyImageIn(proto, inner)
		}
		return !found
	})
	return found
}

// swapImageBlocks swaps the images among a turn's blocks, and those in an
// Anthropic tool_result's, which are a tool's.
func swapImageBlocks(proto provider.Protocol, raw json.RawMessage, last, tool bool, swap func(imageAt) (json.RawMessage, error)) (json.RawMessage, bool, error) {
	var blocks []json.RawMessage
	if decodeJSON(raw, &blocks) != nil {
		return raw, false, nil
	}
	swapped := false
	for i, rawBlock := range blocks {
		var block map[string]json.RawMessage
		if decodeJSON(rawBlock, &block) != nil {
			continue
		}
		var kind string
		decodeJSON(block["type"], &kind)
		src, isImage := imageSrc(proto, kind, block)
		if isImage {
			out, err := swap(imageAt{Src: src, Last: last, Tool: tool, Block: rawBlock})
			if err != nil {
				return raw, false, err
			}
			if out != nil {
				blocks[i] = out
				swapped = true
			}
			continue
		}
		// Gemini's function response holds a tool's images in its own parts
		if proto == provider.Gemini && len(block["functionResponse"]) > 0 {
			var fr map[string]json.RawMessage
			if decodeJSON(block["functionResponse"], &fr) == nil && len(fr["parts"]) > 0 {
				content, nested, err := swapImageBlocks(proto, fr["parts"], last, true, swap)
				if err != nil {
					return raw, false, err
				}
				if nested {
					fr["parts"] = content
					block["functionResponse"], _ = json.Marshal(fr)
					blocks[i], _ = json.Marshal(block)
					swapped = true
				}
			}
		}
		if proto == provider.Anthropic && kind == "tool_result" {
			content, nested, err := swapImageBlocks(proto, block["content"], last, true, swap)
			if err != nil {
				return raw, false, err
			}
			if nested {
				block["content"] = content
				blocks[i], _ = json.Marshal(block)
				swapped = true
			}
		}
	}
	if !swapped {
		return raw, false, nil
	}
	out, _ := json.Marshal(blocks)
	return out, true, nil
}

// imageSrc is whether a block is an image, and the image as a URL when it
// can be read from the block.
func imageSrc(proto provider.Protocol, kind string, block map[string]json.RawMessage) (string, bool) {
	switch proto {
	case provider.Chat:
		if kind != "image_url" {
			return "", false
		}
		var url string
		if decodeJSON(block["image_url"], &url) != nil {
			var obj struct {
				URL string `json:"url"`
			}
			decodeJSON(block["image_url"], &obj)
			url = obj.URL
		}
		return url, true
	case provider.Responses:
		if kind != "input_image" {
			return "", false
		}
		var url string
		decodeJSON(block["image_url"], &url)
		return url, true
	case provider.Anthropic:
		if kind != "image" {
			return "", false
		}
		var src struct {
			Type      string `json:"type"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
			URL       string `json:"url"`
		}
		decodeJSON(block["source"], &src)
		switch {
		case src.Type == "base64" && src.Data != "":
			return "data:" + src.MediaType + ";base64," + src.Data, true
		case src.Type == "url":
			return src.URL, true
		}
		return "", true
	case provider.Gemini:
		var data struct {
			MimeType string `json:"mimeType"`
			Data     string `json:"data"`
		}
		decodeJSON(block["inlineData"], &data)
		if strings.HasPrefix(data.MimeType, "image/") {
			return "data:" + data.MimeType + ";base64," + data.Data, true
		}
		if len(block["fileData"]) > 0 {
			var file struct {
				MimeType string `json:"mimeType"`
			}
			if decodeJSON(block["fileData"], &file) != nil || file.MimeType == "" || strings.HasPrefix(file.MimeType, "image/") {
				return "", true // unknown file types may be images; fail closed
			}
		}
	}
	return "", false
}

// textBlock is a text block of the protocol's.
func textBlock(proto provider.Protocol, text string) json.RawMessage {
	var b []byte
	switch proto {
	case provider.Responses:
		b, _ = json.Marshal(map[string]string{"type": "input_text", "text": text})
	case provider.Gemini:
		b, _ = json.Marshal(map[string]string{"text": text})
	default:
		b, _ = json.Marshal(map[string]string{"type": "text", "text": text})
	}
	return b
}
