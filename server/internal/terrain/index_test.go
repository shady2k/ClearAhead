package terrain

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
)

// переборНеарест — прежняя реализация: честный перебор всех точек оси.
// Оставлена только в тесте: индекс обязан давать тот же ответ, иначе он не
// ускорение, а другая функция.
func (f *Field) переборНеарест(x, y float64) (float64, float64, bool) {
	best := math.Inf(1)
	var bestZ float64
	for _, p := range f.axis {
		dx, dy := x-p.X, y-p.Y
		if d2 := dx*dx + dy*dy; d2 < best {
			best, bestZ = d2, p.Z
		}
	}
	d := math.Sqrt(best)
	if d > f.reach {
		return 0, 0, false
	}
	return d, bestZ, true
}

// Индекс обязан совпадать с перебором ВЕЗДЕ, а не в среднем: расхождение дало
// бы уступ в рельефе там, где запрос попал на границу ячейки.
func TestИндексСовпадаетСПеребором(t *testing.T) {
	m := загрузитьКарту(t)
	f, _ := поле(t, m)

	// Сетка запросов покрывает станцию и выходит за неё, а шаг выбран НЕ
	// кратным стороне ячейки: иначе все запросы легли бы на границы ячеек
	// одинаково и проверка пропустила бы целый класс ошибок.
	const шаг = 7.3
	проверено, вблизи := 0, 0
	for x := -400.0; x <= 1800; x += шаг {
		for y := -300.0; y <= 300; y += шаг {
			d1, z1, ok1 := f.переборНеарест(x, y)
			d2, z2, ok2 := f.nearestAxis(x, y)
			if ok1 != ok2 || d1 != d2 || z1 != z2 {
				t.Fatalf("(%v, %v): перебор (%v, %v, %v), индекс (%v, %v, %v)",
					x, y, d1, z1, ok1, d2, z2, ok2)
			}
			проверено++
			if ok1 {
				вблизи++
			}
		}
	}
	t.Logf("совпало запросов: %d, из них вблизи пути: %d", проверено, вблизи)
	if вблизи == 0 {
		t.Fatal("ни один запрос не попал в зону земляных работ — проверка ничего не доказала")
	}
}

func BenchmarkЧанк(b *testing.B) {
	t := &testing.T{}
	m := загрузитьКарту(t)
	f, _ := поле(t, m)
	a := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 0}
	b.ResetTimer()
	for range b.N {
		if _, err := f.ChunkHeights(a); err != nil {
			b.Fatal(err)
		}
	}
}
