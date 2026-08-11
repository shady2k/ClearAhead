package mapfmt_test

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// Красный корпус: каждое правило валидатора доказано картой, которая его
// нарушает, и ожидаемым текстом отказа. Удаление правила из валидатора делает
// красным ровно свой случай — за это корпус и держат.
//
// Карты строятся кодом: до 2026-08-11 корпус лежал парами testdata/red/*.json и
// *.want, и пара разъезжалась молча — файл правили, ожидание нет. Теперь порча
// и её ожидаемая причина стоят рядом в одной строке таблицы.
//
// Тексты отказов записаны целиком, с числами: «радиус мал» доказывает, что
// правило сработало, но не то, что автору карты сказали, какой радиус и куда
// мал. Число в ожидании ловит и подмену профиля, и сдвиг формата отказа.
func TestКрасныйКорпус(t *testing.T) {
	случаи := []struct {
		имя     string
		карта   *mapfmt.Map
		причина string
	}{
		{
			// Оси пересекаются в (50,0), топология пару не связывает.
			"пересечение осей без устройства",
			двеКомпоненты(
				mapfmt.Anchor{},
				[]mapfmt.HPrim{прямая(300)},
				mapfmt.Anchor{X: 50, Y: -50, Heading: math.Pi / 2},
				[]mapfmt.HPrim{прямая(100)}),
			"оси пересекаются",
		},
		{
			// Ребро уходит в порт, которого в карте нет.
			"ребро на несуществующем порту",
			seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) {
				m.Topology.Edges[0].To = "N9.P1"
			})),
			"несуществующий порт",
		},
		{
			// Конец линии без назначения: ни упор, ни граница карты.
			"висящий конец без назначения",
			seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) {
				for i := range m.Topology.Nodes {
					for j := range m.Topology.Nodes[i].Ports {
						m.Topology.Nodes[i].Ports[j].Purpose = ""
					}
				}
			})),
			"висящий конец",
		},
		{
			"уклон выше предела профиля",
			seedmap.Line(геометрияПерегона(
				[]mapfmt.HPrim{прямая(seedmap.LineLengthM)},
				[]mapfmt.VPrim{{Kind: "grade", Length: seedmap.LineLengthM, SlopePermille: 50}})),
			"уклон 50‰ превышает предел 30‰",
		},
		{
			// Рецепт решётки снят: предмет — радиус, а не покрытие ребра run'ом,
			// которое дуга иной длины сломала бы заодно.
			"радиус ниже минимума профиля",
			seedmap.Line(seedmap.WithoutConstruction(),
				геометрияПерегона([]mapfmt.HPrim{дуга(100, 0.2)}, nil)),
			"радиус 100.0 м меньше минимального 180.0 м",
		},
		{
			"путевой объект на несуществующем элементе",
			seedmap.Line(seedmap.WithTrackside(mapfmt.Trackside{
				ID:   "TS1",
				Kind: "platform",
				Span: netloc.LinearU{{Element: "E9", From: 0, To: 10}},
			})),
			"несуществующий элемент",
		},
	}
	for _, c := range случаи {
		t.Run(c.имя, func(t *testing.T) { отвергает(t, c.карта, c.причина) })
	}
}
