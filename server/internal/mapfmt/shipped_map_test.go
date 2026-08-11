package mapfmt

import (
	"os"
	"path/filepath"
	"testing"
)

// Карта, которая едет в репозитории, обязана проходить полный вход: разбор и
// валидацию. До 2026-08-11 её не грузил ни один тест — вся проверка держалась
// на том, что кто-то поднимет сервер руками. Смена версии формата или правило
// валидатора могли сломать её незаметно для набора тестов, и один такой случай
// уже был: пример из первой редакции спеки отвергался собственным правилом
// спеки, и это поймали только при реализации валидатора.
func TestКартаРепозиторияПроходитВход(t *testing.T) {
	path := filepath.Join("..", "..", "maps", "st_a.json")

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("карта репозитория не открывается: %v", err)
	}
	defer f.Close()

	m, err := Decode(f)
	if err != nil {
		t.Fatalf("разбор %s: %v", path, err)
	}
	if m.FormatVersion != FormatVersion {
		t.Fatalf("версия формата карты %d, код ожидает %d", m.FormatVersion, FormatVersion)
	}
	if err := Validate(m); err != nil {
		t.Fatalf("валидация %s: %v", path, err)
	}

	// Решётка есть и направлена: у run'а направление обязательно у каждого
	// спана, и это ровно то различие, ради которого заводился общий тип.
	if m.Construction == nil || len(m.Construction.Runs) == 0 {
		t.Fatal("в карте нет ни одного run'а решётки")
	}
	for _, r := range m.Construction.Runs {
		if !r.Spans.Directed() {
			t.Fatalf("run %s: не у всех спанов задано направление", r.ID)
		}
	}

	// У платформы направления нет по существу — проверяем, что общий тип это
	// допускает, а не требует заполнять поле ради формы.
	for _, ts := range m.Topology.Trackside {
		if ts.Kind != "platform" {
			continue
		}
		for _, iv := range ts.Span {
			if iv.Direction.Directed() {
				t.Fatalf("платформа %s: у интервала на %s задано направление %q, хотя у платформы его нет",
					ts.ID, iv.Element, iv.Direction)
			}
		}
	}
}
