package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestGatewayStartsGetUserPath: every way uni-switch starts with a gateway
// gives what it starts the node a version manager put on PATH only in the
// shell's profile (#2685, #3803). uni-switch tui runs in a terminal, with its
// PATH already. A start counts when it calls proc.UserPath, or calls a
// function or func var of this package that does (runGUI's openApp, which
// a test stands in for).
func TestGatewayStartsGetUserPath(t *testing.T) {
	want := map[string]bool{"runGUI": false, "runPanel": false, "webCmd": false, "serveCmd": false}
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	direct := map[string]bool{}    // calls proc.UserPath itself
	calls := map[string][]string{} // the package's names it calls
	look := func(name string, body ast.Node) {
		ast.Inspect(body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := n.X.(*ast.Ident); ok && id.Name == "proc" && n.Sel.Name == "UserPath" {
					direct[name] = true
				}
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok {
					calls[name] = append(calls[name], id.Name)
				}
			}
			return true
		})
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Body != nil && d.Recv == nil {
					look(d.Name.Name, d.Body)
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					vs, ok := sp.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, v := range vs.Values {
						if fl, ok := v.(*ast.FuncLit); ok && i < len(vs.Names) {
							look(vs.Names[i].Name, fl.Body)
						}
					}
				}
			}
		}
	}
	for name := range want {
		want[name] = direct[name]
		for _, c := range calls[name] {
			want[name] = want[name] || direct[c]
		}
	}
	for name, ok := range want {
		if !ok {
			t.Errorf("%s doesn't call proc.UserPath", name)
		}
	}
}
