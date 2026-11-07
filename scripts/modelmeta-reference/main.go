// Extract the public preset data from a pinned source file, without importing
// or executing the reference project. Usage: go run ... presets.go output.json
package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		panic("expected presets source and output path")
	}
	f, err := parser.ParseFile(token.NewFileSet(), os.Args[1], nil, 0)
	if err != nil {
		panic(err)
	}
	constants := map[string]ast.Expr{}
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok {
			for _, s := range g.Specs {
				if v, ok := s.(*ast.ValueSpec); ok {
					for i, n := range v.Names {
						if i < len(v.Values) {
							constants[n.Name] = v.Values[i]
						}
					}
				}
			}
		}
	}
	var value func(ast.Expr) any
	value = func(e ast.Expr) any {
		switch x := e.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				s, _ := strconv.Unquote(x.Value)
				return s
			}
		case *ast.Ident:
			if x.Name == "true" {
				return true
			}
			if v := constants[x.Name]; v != nil {
				return value(v)
			}
			return x.Name
		case *ast.CompositeLit:
			keyed := false
			for _, v := range x.Elts {
				if _, ok := v.(*ast.KeyValueExpr); ok {
					keyed = true
					break
				}
			}
			if keyed {
				m := map[string]any{}
				for _, v := range x.Elts {
					if kv, ok := v.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok {
							m[k.Name] = value(kv.Value)
						}
					}
				}
				return m
			}
			a := []any{}
			for _, v := range x.Elts {
				a = append(a, value(v))
			}
			return a
		case *ast.CallExpr:
			if fn, ok := x.Fun.(*ast.Ident); ok && fn.Name == "bailianWorkspace" && len(x.Args) == 1 {
				return "https://{WorkspaceId}." + value(x.Args[0]).(string) + ".maas.aliyuncs.com/compatible-mode/v1"
			}
		}
		return nil
	}
	rows := value(constants["presets"]).([]any)
	out := []map[string]any{}
	for _, row := range rows {
		m := row.(map[string]any)
		n := map[string]any{}
		for _, k := range strings.Fields("ID Kind Hosts Catalog Chat Responses Anthropic Regions") {
			if v, ok := m[k]; ok {
				n[k] = v
			}
		}
		if rs, ok := n["Regions"].([]any); ok {
			regions := []map[string]any{}
			for _, r := range rs {
				rm := r.(map[string]any)
				q := map[string]any{}
				for _, k := range strings.Fields("Catalog Chat Responses Anthropic") {
					if v, ok := rm[k]; ok {
						q[k] = v
					}
				}
				regions = append(regions, q)
			}
			n["Regions"] = regions
		}
		out = append(out, n)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(os.Args[2], append(b, '\n'), 0644); err != nil {
		panic(err)
	}
}
