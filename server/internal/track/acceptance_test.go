package track

import (
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// TestStationCompiles — станция фабрики проходит весь путь: валидация,
// компиляция, замыкание. Проверка сквозная и потому здесь, а не в тестах
// отдельных модулей: отказ на любом шаге обесценивает всё остальное.
func TestStationCompiles(t *testing.T) {
	m := seedmap.Station()
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("валидация: %v", err)
	}
	cn, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция (замыкание не сошлось?): %v", err)
	}
	// Две — это горловина: SW1 и SW2 образуют съезд. Одной стрелки мало, на
	// такой карте не воспроизводится наложение шпальных решёток соседних
	// элементов, а оно живое и его придётся ловить.
	if len(m.Topology.Turnouts) != 2 {
		t.Fatalf("стрелок %d, у горловины две", len(m.Topology.Turnouts))
	}
	if len(cn.Elements) != len(rg.Elements) {
		t.Fatalf("элементов в CompiledNetwork %d, в RenderGeometry %d", len(cn.Elements), len(rg.Elements))
	}
	// Станция плоская: пространственная длина совпадает с плановой.
	for id, e := range cn.Elements {
		if e.LengthS != e.LengthU {
			t.Fatalf("%s: s=%s u=%s, станция объявлена плоской", id, e.LengthS, e.LengthU)
		}
	}
	t.Logf("станция скомпилирована: %d элементов, %d устройств, %d сооружений",
		len(cn.Elements), len(cn.Devices), len(cn.Structures))
}
