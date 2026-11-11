package pluginrt

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// A plugin's settings are a struct (Manifest.Settings): each exported
// field with a json name is one setting, its value in the struct the
// default. The window draws their form from the Schema made of it (Plugins
// › Built-in, kernel.js's ui.form), and SetConfig takes only what the
// schema allows, so the form, plugins.yml and `uni-switch plugin dump` can't
// disagree on what a setting is.
//
//	type Settings struct {
//		CallsPage int `json:"callsPage" title:"Recent calls at a time" description:"…" enum:"10,20,50"`
//	}
//
// Tags: title (the row's name, in English; the window translates it),
// description, enum (the values allowed, comma separated), min and max
// (for a number).

// Schema is a plugin's settings as JSON Schema says an object: the window
// draws a form from it. Order is the fields' order, as JSON keeps no order
// of an object's keys.
type Schema struct {
	Type       string               `json:"type"` // "object"
	Properties map[string]*Property `json:"properties"`
	Order      []string             `json:"x-order"`
}

// Property is one setting.
type Property struct {
	Type        string   `json:"type"` // boolean, integer, number, string
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Enum        []any    `json:"enum,omitempty"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
	Default     any      `json:"default"`
}

// SchemaOf is the schema of the settings struct v, and its defaults. A
// field of a kind a form can't draw, or a tag that doesn't parse, is a
// programming error.
func SchemaOf(v any) (*Schema, map[string]any) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Struct {
		panic(fmt.Sprintf("pluginrt: settings %T is not a struct", v))
	}
	s := &Schema{Type: "object", Properties: map[string]*Property{}}
	defaults := map[string]any{}
	for i := 0; i < rv.NumField(); i++ {
		f := rv.Type().Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if !f.IsExported() || name == "" || name == "-" {
			continue
		}
		p := &Property{Title: f.Tag.Get("title"), Description: f.Tag.Get("description")}
		if p.Title == "" {
			panic(fmt.Sprintf("pluginrt: setting %s has no title", name))
		}
		switch f.Type.Kind() {
		case reflect.Bool:
			p.Type, p.Default = "boolean", rv.Field(i).Bool()
		case reflect.Int, reflect.Int32, reflect.Int64:
			p.Type, p.Default = "integer", rv.Field(i).Int()
		case reflect.Float64:
			p.Type, p.Default = "number", rv.Field(i).Float()
		case reflect.String:
			p.Type, p.Default = "string", rv.Field(i).String()
		default:
			panic(fmt.Sprintf("pluginrt: setting %s is a %s, which a form can't draw", name, f.Type))
		}
		if e := f.Tag.Get("enum"); e != "" {
			for _, w := range strings.Split(e, ",") {
				x, err := p.parse(strings.TrimSpace(w))
				if err != nil {
					panic(fmt.Sprintf("pluginrt: setting %s's enum: %v", name, err))
				}
				p.Enum = append(p.Enum, x)
			}
		}
		for tag, to := range map[string]**float64{"min": &p.Minimum, "max": &p.Maximum} {
			if w := f.Tag.Get(tag); w != "" {
				x, err := strconv.ParseFloat(w, 64)
				if err != nil {
					panic(fmt.Sprintf("pluginrt: setting %s's %s: %v", name, tag, err))
				}
				*to = &x
			}
		}
		if err := p.check(p.Default); err != nil {
			panic(fmt.Sprintf("pluginrt: setting %s's default: %v", name, err))
		}
		s.Properties[name] = p
		s.Order = append(s.Order, name)
		defaults[name] = p.Default
	}
	return s, defaults
}

// parse reads w as a value of p's type, for an enum tag.
func (p *Property) parse(w string) (any, error) {
	switch p.Type {
	case "integer":
		return strconv.ParseInt(w, 10, 64)
	case "number":
		return strconv.ParseFloat(w, 64)
	case "boolean":
		return strconv.ParseBool(w)
	}
	return w, nil
}

// check says whether v may be p's value, as it comes from JSON or YAML (a
// number is a float64 from JSON, an int from YAML), and gives it as p's
// type.
func (p *Property) check(v any) error {
	_, err := p.value(v)
	return err
}

func (p *Property) value(v any) (any, error) {
	var out any
	switch p.Type {
	case "boolean":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("%v is not true or false", v)
		}
		out = b
	case "string":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%v is not text", v)
		}
		out = s
	case "integer", "number":
		var f float64
		switch n := v.(type) {
		case int:
			f = float64(n)
		case int64:
			f = float64(n)
		case float64:
			f = n
		default:
			return nil, fmt.Errorf("%v is not a number", v)
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("%v is not a number", v)
		}
		if p.Minimum != nil && f < *p.Minimum || p.Maximum != nil && f > *p.Maximum {
			return nil, fmt.Errorf("%v is out of range", v)
		}
		if p.Type == "integer" {
			if f != math.Trunc(f) {
				return nil, fmt.Errorf("%v is not a whole number", v)
			}
			out = int64(f)
		} else {
			out = f
		}
	}
	if len(p.Enum) > 0 && !slices.Contains(p.Enum, out) {
		return nil, fmt.Errorf("%v is not one of %v", v, p.Enum)
	}
	return out, nil
}

// Validate gives config as the schema's types, or says the first key or
// value it doesn't allow.
func (s *Schema) Validate(config map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for k, v := range config {
		p := s.Properties[k]
		if p == nil {
			return nil, fmt.Errorf("no setting %q", k)
		}
		x, err := p.value(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		out[k] = x
	}
	return out, nil
}
