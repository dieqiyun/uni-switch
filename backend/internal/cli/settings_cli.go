package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/settingsapi"
)

const settingsUsage = `  uni-switch settings [list] [--json]          every setting the window's Settings saves: its value now and its default
  uni-switch settings get <key> [--json]       one setting's value
  uni-switch settings set <key> <value>        set one, checked and saved as the window saves it
  uni-switch settings reset <key>              one back to its default

  a value is on|off (or true|false) for a switch, a number, words, a list as a,b,c
  (or a JSON array), and JSON for an object; a key with a closed set lists its choices.
  Set while the app is open, a setting is read from the file as the app reads it; the
  app's Dock, fonts and tray menu follow a change made here when it starts next.
`

// settingsCmd is `uni-switch settings …` (#4745): the settings POST
// /api/settings saves, read and written through settingsapi, the same
// save the window's Settings makes.
func settingsCmd(args []string) error {
	asJSON := false
	var rest []string
	for _, a := range args {
		switch {
		case a == "--json" || a == "-j":
			asJSON = true
		case strings.HasPrefix(a, "-") && !isNumber(a):
			// a flag is never taken for a key or a value to write (#1457)
			return fmt.Errorf("unknown flag %s: nothing was changed\n\n%s", a, settingsUsage)
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 {
		rest = []string{"list"}
	}
	switch rest[0] {
	case "list", "ls":
		if len(rest) != 1 {
			return fmt.Errorf("usage: uni-switch settings list [--json]")
		}
		return settingsList(asJSON)
	case "get":
		if len(rest) != 2 {
			return fmt.Errorf("usage: uni-switch settings get <key> [--json]")
		}
		return settingsGet(rest[1], asJSON)
	case "set":
		if len(rest) != 3 {
			return fmt.Errorf("usage: uni-switch settings set <key> <value>: nothing was changed")
		}
		return settingsSet(rest[1], rest[2], false)
	case "reset":
		if len(rest) != 2 {
			return fmt.Errorf("usage: uni-switch settings reset <key>: nothing was changed")
		}
		return settingsSet(rest[1], "", true)
	}
	return fmt.Errorf("unknown settings command %q\n\n%s", rest[0], settingsUsage)
}

func isNumber(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// settingRow is one setting as `uni-switch settings list --json` gives it.
type settingRow struct {
	Key     string   `json:"key"`
	Type    string   `json:"type"`
	Value   any      `json:"value"`
	Default any      `json:"default"`
	Choices []string `json:"choices,omitempty"`
}

func settingRows() ([]settingRow, error) {
	cur, err := settings.LoadChecked()
	if err != nil {
		return nil, err
	}
	def := settings.Defaults()
	var rows []settingRow
	for _, k := range settingsapi.Keys {
		f, _ := settingsapi.Field(k)
		v, _ := settingsapi.Value(cur, k)
		d, _ := settingsapi.Value(def, k)
		rows = append(rows, settingRow{Key: k, Type: typeWord(f.Type), Value: v, Default: d, Choices: settingsapi.Choices[k]})
	}
	return rows, nil
}

func settingsList(asJSON bool) error {
	rows, err := settingRows()
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(rows)
	}
	w := 0
	for _, r := range rows {
		w = max(w, len(r.Key))
	}
	for _, r := range rows {
		v, d := shown(r.Value), shown(r.Default)
		line := fmt.Sprintf("%-*s  %s", w, r.Key, v)
		note := r.Type
		if len(r.Choices) > 0 {
			note = strings.Join(r.Choices, "|")
		}
		if v != d {
			note += ", default " + d
		}
		fmt.Println(line, muted.Render("("+note+")"))
	}
	return nil
}

func settingsGet(key string, asJSON bool) error {
	if err := knownSetting(key); err != nil {
		return err
	}
	cur, err := settings.LoadChecked()
	if err != nil {
		return err
	}
	v, _ := settingsapi.Value(cur, key)
	if asJSON {
		return printJSON(v)
	}
	fmt.Println(shown(v))
	return nil
}

// settingsSet saves key as text says, or as its default when reset,
// through settingsapi.Apply: every other setting stays as it is, and a
// value that doesn't check out writes nothing.
func settingsSet(key, text string, reset bool) error {
	if err := knownSetting(key); err != nil {
		return err
	}
	f, _ := settingsapi.Field(key)
	var value json.RawMessage
	if reset {
		// the default as a new install has it; a list's as an empty one,
		// which a list the save keeps when it is not sent (searcherThen)
		// needs to be cleared
		d, _ := settingsapi.Value(settings.Defaults(), key)
		b, err := json.Marshal(d)
		if err != nil {
			return err
		}
		if string(b) == "null" && f.Type.Kind() == reflect.Slice {
			b = []byte("[]")
		}
		value = b
	} else {
		v, err := settingValue(key, f.Type, text)
		if err != nil {
			return fmt.Errorf("%w: nothing was changed", err)
		}
		value = v
	}
	cur, err := settings.LoadChecked()
	if err != nil {
		return err
	}
	body, err := settingsapi.Body(cur, key, value)
	if err != nil {
		return err
	}
	if _, _, err := settingsapi.Apply(body, false); err != nil {
		return fmt.Errorf("%w: nothing was changed", err)
	}
	// as saved, which the save may have put in its own form (06:00)
	v, _ := settingsapi.Value(settings.Load(), key)
	fmt.Println(green.Render("✓"), bold.Render(key), shown(v))
	return nil
}

func knownSetting(key string) error {
	if settingsapi.Known(key) {
		return nil
	}
	var near []string
	for _, k := range settingsapi.Keys {
		if strings.Contains(strings.ToLower(k), strings.ToLower(key)) || strings.Contains(strings.ToLower(key), strings.ToLower(k)) {
			near = append(near, k)
		}
	}
	msg := fmt.Sprintf("no setting %q", key)
	if len(near) > 0 {
		msg += ": did you mean " + strings.Join(near, ", ") + "?"
	}
	return fmt.Errorf("%s (uni-switch settings list names them all): nothing was changed", msg)
}

// settingValue is text as the JSON a setting of type t is saved as.
func settingValue(key string, t reflect.Type, text string) (json.RawMessage, error) {
	text = strings.TrimSpace(text)
	base := t
	if base.Kind() == reflect.Pointer {
		base = base.Elem()
	}
	var out any
	switch base.Kind() {
	case reflect.Bool:
		switch strings.ToLower(text) {
		case "on", "true", "yes", "1":
			out = true
		case "off", "false", "no", "0":
			out = false
		default:
			return nil, fmt.Errorf("%s is on or off, not %q", key, text)
		}
	case reflect.Int, reflect.Int64, reflect.Int32:
		n, err := strconv.Atoi(text)
		if err != nil {
			return nil, fmt.Errorf("%s is a whole number, not %q", key, text)
		}
		out = n
	case reflect.Float64, reflect.Float32:
		n, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("%s is a number, not %q", key, text)
		}
		out = n
	case reflect.String:
		out = text
	case reflect.Slice:
		if base.Elem().Kind() == reflect.String && !strings.HasPrefix(text, "[") {
			list := []string{}
			for v := range strings.SplitSeq(text, ",") {
				if v = strings.TrimSpace(v); v != "" {
					list = append(list, v)
				}
			}
			out = list
			break
		}
		fallthrough
	default:
		// an object or a list of them: JSON, every field one it has
		v := reflect.New(t)
		dec := json.NewDecoder(strings.NewReader(text))
		dec.DisallowUnknownFields()
		if err := dec.Decode(v.Interface()); err != nil {
			return nil, fmt.Errorf("%s is JSON (%s): %v", key, typeWord(t), err)
		}
		if dec.More() {
			return nil, fmt.Errorf("%s is one JSON value", key)
		}
		return json.RawMessage(text), nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	// what it is decoded into checks it once more (an int's range)
	if err := json.Unmarshal(b, reflect.New(t).Interface()); err != nil {
		return nil, fmt.Errorf("%s: %v", key, err)
	}
	return b, nil
}

// typeWord names t for the list: on|off, number, text, list or JSON.
func typeWord(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Bool:
		return "on|off"
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Float64, reflect.Float32:
		return "number"
	case reflect.String:
		return "text"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String {
			return "list"
		}
	}
	return "JSON"
}

// shown is a value as list and get print it: a switch on or off, words
// as they are, anything else as JSON.
func shown(v any) string {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "-"
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Bool:
		if rv.Bool() {
			return "on"
		}
		return "off"
	case reflect.String:
		if rv.String() == "" {
			return `""`
		}
		return rv.String()
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.String {
			if rv.Len() == 0 {
				return "[]"
			}
			return strings.Join(rv.Interface().([]string), ",")
		}
	case reflect.Map:
		if rv.Len() == 0 {
			return "{}"
		}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rv.Interface()); err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimSpace(b.String())
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
