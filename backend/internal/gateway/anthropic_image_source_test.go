package gateway

import (
	"encoding/json"

	"reflect"

	"testing"
)

func TestAnthropicImageSourceFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		part Part
		want map[string]string
	}{
		{"base64", Part{Kind: Image, MediaType: "image/png", Data: pngA}, map[string]string{"type": "base64", "media_type": "image/png", "data": pngA}},
		{"dataURL", Part{Kind: Image, MediaType: "image/png", Data: pngA, URL: "data:image/png;base64," + pngA}, map[string]string{"type": "base64", "media_type": "image/png", "data": pngA}},
		{"url", Part{Kind: Image, URL: "https://example.test/screenshot.png"}, map[string]string{"type": "url", "url": "https://example.test/screenshot.png"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(imageBlock(tc.part))
			if err != nil {
				t.Fatal(err)
			}
			var block struct {
				Type   string            `json:"type"`
				Source map[string]string `json:"source"`
			}
			if err := json.Unmarshal(body, &block); err != nil {
				t.Fatal(err)
			}
			if block.Type != "image" || !reflect.DeepEqual(block.Source, tc.want) {
				t.Fatalf("image source includes missing or extra fields: %s; want %v", body, tc.want)
			}
		})
	}
}
