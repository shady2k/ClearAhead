package rpc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// Запрещённые вне internal/rpc обращения к сырому внешнему входу. Если они
// появились — значит кто-то читает запрос мимо барьера.
var forbidden = []string{"PathValue", "Unmarshal", "NewDecoder", "ParseForm"}

func TestBarrierNoRawInputOutsideRPC(t *testing.T) {
	roots := []string{"../httpapi", "../track", "../mapfmt", "../../cmd"}
	for _, root := range roots {
		matches, err := filepath.Glob(filepath.Join(root, "*.go"))
		if err != nil {
			t.Fatalf("обход %s: %v", root, err)
		}
		for _, path := range matches {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			// mapfmt разбирает файл карты с диска, а не внешний вызов: это
			// другой вход, у него свой строгий разбор (Задача 3).
			if strings.Contains(path, "mapfmt") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("разбор %s: %v", path, err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				for _, bad := range forbidden {
					if sel.Sel.Name == bad {
						t.Errorf("%s: %s вне internal/rpc — внешний вход читается мимо барьера валидации",
							fset.Position(sel.Pos()), bad)
					}
				}
				return true
			})
		}
	}
}
