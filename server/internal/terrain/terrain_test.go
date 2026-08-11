package terrain

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

func загрузитьКарту(t *testing.T) *mapfmt.Map {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "maps", "st_a.json"))
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
	return m
}

func поле(t *testing.T, m *mapfmt.Map) (*Field, map[string]track.Element) {
	t.Helper()
	_, els, err := track.Propagate(m)
	if err != nil {
		t.Fatalf("распространение поз: %v", err)
	}
	f, err := New(m, els)
	if err != nil {
		t.Fatalf("построение рельефа: %v", err)
	}
	return f, els
}

// Приёмочный критерий биды: высоты согласованы с уклоном пути. Под осью земля
// обязана лежать на отметке оси — иначе путь висел бы в воздухе или тонул.
func TestПодОсьюЗемляНаОтметкеПути(t *testing.T) {
	m := загрузитьКарту(t)
	f, els := поле(t, m)

	проверено := 0
	for id, e := range els {
		pts, err := sampleAxis(e, nil)
		if err != nil {
			t.Fatalf("%s: выборка оси: %v", id, err)
		}
		for _, p := range pts {
			got := f.WorkedM(p.X, p.Y)
			if math.Abs(got-p.Z) > 1e-9 {
				t.Fatalf("%s: под осью в (%.3f, %.3f) земля на %.6f, ось на %.6f",
					id, p.X, p.Y, got, p.Z)
			}
			проверено++
		}
	}
	if проверено == 0 {
		t.Fatal("не проверено ни одной точки")
	}
	t.Logf("проверено точек оси: %d", проверено)
}

// Вдали от пути земля природная: земляные работы не должны доставать до
// горизонта.
func TestВдалиОтПутиЗемляПриродная(t *testing.T) {
	m := загрузитьКарту(t)
	f, _ := поле(t, m)

	x, y := 50000.0, 50000.0
	if got, want := f.WorkedM(x, y), f.NaturalM(x, y); got != want {
		t.Fatalf("вдали от пути земля %v, природная %v", got, want)
	}
}

// Рецепт с одной затравкой даёт один рельеф; с разной — разный. Без первого
// карта не воспроизводима, без второго затравка не работает.
func TestЗатравкаОпределяетРельеф(t *testing.T) {
	m := загрузитьКарту(t)
	f1, _ := поле(t, m)

	a := f1.NaturalM(1234.5, -678.9)
	b := f1.NaturalM(1234.5, -678.9)
	if a != b {
		t.Fatalf("один вызов дал %v, другой %v", a, b)
	}

	m2 := загрузитьКарту(t)
	m2.Terrain.Seed++
	f2, _ := поле(t, m2)
	if f2.NaturalM(1234.5, -678.9) == a {
		t.Fatal("смена затравки не изменила рельеф")
	}
}

// Квантование — не украшение, а условие переносимости между машинами: даже при
// расхождении в последних битах сантиметр совпадёт. Проверяем, что округление
// происходит ровно один раз и от рабочей высоты.
func TestКвантованиеВСантиметры(t *testing.T) {
	m := загрузитьКарту(t)
	f, _ := поле(t, m)

	for _, p := range [][2]float64{{0, 0}, {150.25, -3.5}, {-800, 900}} {
		cm, err := f.HeightCm(p[0], p[1])
		if err != nil {
			t.Fatalf("(%v, %v): %v", p[0], p[1], err)
		}
		want := int16(math.Round((f.WorkedM(p[0], p[1]) - m.Terrain.BaseZ) * 100))
		if cm != want {
			t.Fatalf("(%v, %v): отсчёт %d, ожидалось %d", p[0], p[1], cm, want)
		}
	}
}

// МОСТ И ТОННЕЛЬ. На их протяжении путь несёт сооружение, а земля остаётся
// природной: без этого земляные работы сравняли бы долину под мостом и
// прокопали траншею над тоннелем — авторитет пути применился бы там, где путь
// земли не касается.
//
// Карта здесь одноэлементная намеренно. На станции соседние пути тянут землю к
// себе, и на её геометрии проверка вышла бы неубедительной: земля сдвинулась
// бы, но не до природной. Изолированный перегон даёт однозначный ответ.
func TestПодМостомЗемляОстаётсяПриродной(t *testing.T) {
	for _, kind := range []string{"bridge", "tunnel"} {
		t.Run(kind, func(t *testing.T) {
			m := одноРебро(t, nil)
			безСооружения, els := поле(t, m)

			e := els["E1"]
			pts, err := sampleAxis(e, nil)
			if err != nil {
				t.Fatalf("выборка оси: %v", err)
			}
			p := pts[len(pts)/2]

			// Контроль: без сооружения земля притянута к оси.
			if !математическиРавны(безСооружения.WorkedM(p.X, p.Y), p.Z) {
				t.Fatalf("без сооружения земля %v, ось %v",
					безСооружения.WorkedM(p.X, p.Y), p.Z)
			}
			// И она заметно отличается от природной — иначе проверка ниже
			// прошла бы сама собой на плоском рельефе.
			if математическиРавны(безСооружения.WorkedM(p.X, p.Y), безСооружения.NaturalM(p.X, p.Y)) {
				t.Fatal("природная земля совпала с осью — тест ничего не докажет")
			}

			сСооружением, _ := поле(t, одноРебро(t, &mapfmt.Trackside{
				ID:   "SOORUZHENIE",
				Kind: kind,
				Span: netloc.LinearU{{Element: "E1", From: 0, To: 200}},
			}))

			got := сСооружением.WorkedM(p.X, p.Y)
			want := сСооружением.NaturalM(p.X, p.Y)
			if !математическиРавны(got, want) {
				t.Fatalf("%s: земля %v, ожидалась природная %v", kind, got, want)
			}
		})
	}
}

// одноРебро — минимальная карта: один прямой перегон 200 м с рельефом.
func одноРебро(t *testing.T, ts *mapfmt.Trackside) *mapfmt.Map {
	t.Helper()
	m := &mapfmt.Map{
		FormatVersion: mapfmt.FormatVersion,
		MapID:         "PEREGON",
		MapRevision:   1,
		Anchors:       map[string]mapfmt.Anchor{"NA.P1": {X: 0, Y: 0, Z: 150, Heading: 0}},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: "NA", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: "NB", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
			},
			Edges: []mapfmt.Edge{{ID: "E1", From: "NA.P1", To: "NB.P1"}},
		},
		Geometry: mapfmt.Geometry{
			Turnouts: map[string]mapfmt.TurnoutGeometry{},
			Edges: map[string]mapfmt.Alignments{
				"E1": {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 200}}},
			},
		},
		Terrain: &mapfmt.Terrain{
			Seed:  777,
			BaseZ: 140,
			Octaves: []mapfmt.TerrainOctave{
				{WavelengthM: 400, AmplitudeM: 18},
				{WavelengthM: 90, AmplitudeM: 3},
			},
			Earthworks: mapfmt.Earthworks{FormationHalfWidth: 5, SideSlope: 1.5},
		},
	}
	if ts != nil {
		m.Topology.Trackside = append(m.Topology.Trackside, *ts)
	}
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("минимальная карта отвергнута: %v", err)
	}
	return m
}

func математическиРавны(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
