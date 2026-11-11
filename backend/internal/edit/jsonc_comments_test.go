package edit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/jsonc"
)

func TestJSONCLeadingCommentsKeepMembers(t *testing.T) {
	object := "{\n  \"keep\": {\"nested\": \"original\"},\n  \"label\": \"stable\"\n}\n"
	compact := `{"keep":{"nested":"original"},"label":"stable"}`
	crlf := "{\r\n\t\"keep\": {\"nested\": \"原值\"},\r\n\t\"label\": \"stable\"\r\n}\r\n"
	members := []string{`"nested": "original"`, `"label": "stable"`}
	unchanged := map[string]string{"keep.nested": "original", "label": "stable"}
	unicodeUnchanged := map[string]string{"keep.nested": "原值", "label": "stable"}
	lineHeader := "// heading { brace stays in this comment\n"
	blockHeader := "/* heading { brace stays in this comment */\n"
	nestedInput := lineHeader + blockHeader + object

	cases := []struct {
		name      string
		input     string
		key       string
		value     string
		comments  []string
		members   []string
		unchanged map[string]string
	}{
		{
			name: "plain_top_level_control", input: object, key: "added", value: "new",
			members: members, unchanged: unchanged,
		},
		{
			name: "line_header_brace", input: lineHeader + object, key: "added", value: "new",
			comments: []string{strings.TrimSuffix(lineHeader, "\n")}, members: members, unchanged: unchanged,
		},
		{
			name: "block_header_brace", input: blockHeader + object, key: "added", value: "new",
			comments: []string{strings.TrimSuffix(blockHeader, "\n")}, members: members, unchanged: unchanged,
		},
		{
			name: "compact_block_header_brace", input: "/* heading { brace */" + compact, key: "added", value: "new",
			comments: []string{"/* heading { brace */"},
			members:  []string{`"nested":"original"`, `"label":"stable"`}, unchanged: unchanged,
		},
		{
			name: "tab_unicode_crlf_line_header_brace", input: "\t// 中文说明 { 保留\r\n" + crlf, key: "新增", value: "模型\t更新",
			comments: []string{"\t// 中文说明 { 保留\r\n"},
			members:  []string{"\t\"keep\": {\"nested\": \"原值\"},\r\n", "\t\"label\": \"stable\"\r\n"}, unchanged: unicodeUnchanged,
		},
		{
			name: "tab_unicode_crlf_block_header_brace", input: "\t/* 中文说明 { 保留 */\r\n" + crlf, key: "新增", value: "模型\t更新",
			comments: []string{"\t/* 中文说明 { 保留 */\r\n"},
			members:  []string{"\t\"keep\": {\"nested\": \"原值\"},\r\n", "\t\"label\": \"stable\"\r\n"}, unchanged: unicodeUnchanged,
		},
		{
			name: "empty_line_comment_object", input: "{\n  // inner { comment\n}\n", key: "added", value: "new",
			comments: []string{"// inner { comment"},
		},
		{
			name: "empty_block_comment_object", input: "{\n  /* inner { comment */\n}\n", key: "added", value: "new",
			comments: []string{"/* inner { comment */"},
		},
		{
			name: "compact_empty_block_comment_object", input: "{/* inner { comment */}", key: "added", value: "new",
			comments: []string{"/* inner { comment */"},
		},
		{
			name: "empty_object_line_header_brace", input: lineHeader + "{\n  // keep inner comment\n}\n", key: "added", value: "new",
			comments: []string{strings.TrimSuffix(lineHeader, "\n"), "// keep inner comment"},
		},
		{
			name: "empty_object_block_header_brace", input: blockHeader + "{\n  /* keep inner comment */\n}\n", key: "added", value: "new",
			comments: []string{strings.TrimSuffix(blockHeader, "\n"), "/* keep inner comment */"},
		},
		{
			name: "existing_nested_object_control", input: nestedInput, key: "keep.added", value: "new",
			comments: []string{strings.TrimSuffix(lineHeader, "\n"), strings.TrimSuffix(blockHeader, "\n")},
			members:  members, unchanged: unchanged,
		},
		{
			name: "existing_nested_value_control", input: nestedInput, key: "keep.nested", value: "updated",
			comments: []string{strings.TrimSuffix(lineHeader, "\n"), strings.TrimSuffix(blockHeader, "\n")},
			members:  []string{`"label": "stable"`}, unchanged: map[string]string{"label": "stable"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(tc.input)
			if !json.Valid(jsonc.ToJSON(append([]byte(nil), input...))) {
				t.Fatalf("fixture is not valid JSONC: %q", tc.input)
			}
			path := tmpFile(t, "settings.jsonc", tc.input)
			for key, want := range tc.unchanged {
				if got, ok := GetJSON(path, key); !ok || got != want {
					t.Fatalf("fixture GetJSON(%q) = %q, %v; want %q, true", key, got, ok, want)
				}
			}

			if err := SetJSON(path, KV{Path: tc.key, Value: tc.value}); err != nil {
				t.Fatalf("SetJSON on valid JSONC: %v", err)
			}
			output := []byte(read(t, path))
			valid := json.Valid(jsonc.ToJSON(append([]byte(nil), output...)))
			value, exists := GetJSON(path, tc.key)
			t.Logf("SetJSON returned nil; json.Valid = %v; GetJSON(%q) = %q, %v; output = %q", valid, tc.key, value, exists, output)
			if !valid {
				t.Errorf("SetJSON produced invalid JSONC")
			}
			if !exists || value != tc.value {
				t.Errorf("GetJSON(%q) = %q, %v; want %q, true", tc.key, value, exists, tc.value)
			}
			for _, comment := range tc.comments {
				if !strings.Contains(string(output), comment) {
					t.Errorf("original comment changed: %q", comment)
				}
			}
			for _, member := range tc.members {
				if !strings.Contains(string(output), member) {
					t.Errorf("original member changed: %q", member)
				}
			}
			for key, want := range tc.unchanged {
				if got, ok := GetJSON(path, key); !ok || got != want {
					t.Errorf("original GetJSON(%q) = %q, %v; want %q, true", key, got, ok, want)
				}
			}
		})
	}
}
