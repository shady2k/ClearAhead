package track

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// TestStationCompiles грузит карту-фикстуру из testdata пакета mapfmt. Строка в
// коде проверила бы компилятор, но не карту — а ошибка автора карты и есть то,
// ради чего написан валидатор. Фикстура маленькая: одна стрелка, три ребра.
func TestStationCompiles(t *testing.T) {
	path := filepath.Join("..", "mapfmt", "testdata", "fixture_station.json")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("карта: %v", err)
	}
	defer f.Close()

	m, err := mapfmt.Decode(f)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("валидация: %v", err)
	}
	ct, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция (замыкание не сошлось?): %v", err)
	}
	// Две — это горловина: SW1 и SW2 образуют съезд. Одной стрелки мало, на
	// такой фикстуре не воспроизводится наложение шпальных решёток соседних
	// элементов, а оно живое и его придётся ловить.
	if len(m.Topology.Turnouts) != 2 {
		t.Fatalf("стрелок %d, в фикстуре горловина из двух", len(m.Topology.Turnouts))
	}
	if len(ct.Elements) != len(rg.Elements) {
		t.Fatalf("элементов в CompiledTrack %d, в RenderGeometry %d", len(ct.Elements), len(rg.Elements))
	}
	// Станция плоская: пространственная длина совпадает с плановой.
	for id, e := range ct.Elements {
		if e.LengthS != e.LengthU {
			t.Fatalf("%s: s=%s u=%s, станция объявлена плоской", id, e.LengthS, e.LengthU)
		}
	}
	t.Logf("станция скомпилирована: %d элементов, %d стрелок, %d путевых объектов",
		len(ct.Elements), len(ct.Turnouts), len(ct.Trackside))
}
