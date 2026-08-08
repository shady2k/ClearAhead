package rpc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// Чёрный список конкретных вызовов, которыми легче всего обойти барьер.
//
// ЧЕСТНАЯ ГРАНИЦА, не путать с гарантией. Этот тест НЕ доказывает тезис «сырой
// внешний вход не читается вне rpc»: список закрывает четыре имени, а обойти его
// можно через r.URL.Path, io.ReadAll(r.Body), собственный декодер или helper.
//
// Настоящую гарантию даёт система типов: Register принимает только типы,
// реализующие запечатанный protocol.Request, а обход встраиванием закрыт методом
// native() T и проверяется compile-negative тестом в bypass_test.go. Этот же тест
// — дешёвый сторож против самых вероятных случайностей, не более.
//
// Ручка httpapi механически раскладывает сегменты пути в protocol.Input и отдаёт
// диспетчеру — это не обход: разбор, проверка и решение живут за барьером.
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
