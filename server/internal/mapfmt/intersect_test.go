package mapfmt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twoEdgeMap — две отдельные компоненты по одному ребру, каждая со своим
// якорем. E1 всегда: якорь N1.P1 (0,0), курс 0, геометрия e1. E2: якорь N3.P1
// (50,-50), курс π/2, геометрия e2, если не заменён вызовом.
func twoEdgeMap(e1, e2 string) string {
	return `{
  "format_version": 4, "map_id": "X", "map_revision": 1,
  "anchors": {
    "N1.P1": { "x": 0, "y": 0, "z": 0, "heading": 0 },
    "N3.P1": { "x": 50, "y": -50, "z": 0, "heading": 1.5707963267948966 }
  },
  "topology": {
    "nodes": [
      { "id": "N1", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
      { "id": "N2", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] },
      { "id": "N3", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
      { "id": "N4", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] }
    ],
    "turnouts": [], "trackside": [],
    "edges": [
      { "id": "E1", "from": "N1.P1", "to": "N2.P1" },
      { "id": "E2", "from": "N3.P1", "to": "N4.P1" }
    ]
  },
  "geometry": { "turnouts": {}, "edges": {
    "E1": { "horizontal": [` + e1 + `] },
    "E2": { "horizontal": [` + e2 + `] }
  } }
}`
}

func validateRejects(t *testing.T, doc, want string) {
	t.Helper()
	m, err := Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	err = Validate(m)
	if err == nil {
		t.Fatal("ожидался отказ, получен успех")
	}
	if want != "" && !strings.Contains(err.Error(), want) {
		t.Fatalf("ожидалась ошибка про %q, получено: %v", want, err)
	}
}

func validateAccepts(t *testing.T, doc string) {
	t.Helper()
	m, err := Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if err := Validate(m); err != nil {
		t.Fatalf("карта должна быть валидна: %v", err)
	}
}

// TestValidateAxisRejectsCrossing — синтетическая карта с заведомым
// пересечением осей в середине: E1 идёт на восток по y=0, E2 — на север из
// (50,-50). Пересечение в (50, 0) не объяснено топологией: у пары нет общего
// порта. Это тот самый тест, который обязан быть красным до правки и зелёным
// после.
func TestValidateAxisRejectsCrossing(t *testing.T) {
	validateRejects(t, twoEdgeMap(
		`{ "kind": "straight", "length": 100.0 }`,
		`{ "kind": "straight", "length": 100.0 }`),
		"E1 x E2 в (50.0, 0.0)")
}

// TestValidateAxisRejectsTEnd — конец пути, упирающийся в середину другого без
// топологической связи: E2 длиной 50 от (50,-50) на север кончается в (50, 0),
// на середине E1.
func TestValidateAxisRejectsTEnd(t *testing.T) {
	validateRejects(t, twoEdgeMap(
		`{ "kind": "straight", "length": 100.0 }`,
		`{ "kind": "straight", "length": 50.0 }`),
		"E1 x E2")
}

// TestValidateAxisRejectsTangent — касание без пересечения вне разрешённого
// порта. E1 идёт на север по x=0. E2 — дуга радиуса 100 с центром (0,100) от
// угла 120° до 60° (по часовой); её вершина (0,200) касается середины E1.
func TestValidateAxisRejectsTangent(t *testing.T) {
	doc := `{
  "format_version": 4, "map_id": "T", "map_revision": 1,
  "anchors": {
    "N1.P1": { "x": 0, "y": 0, "z": 0, "heading": 1.5707963267948966 },
    "N3.P1": { "x": -50, "y": 186.60254037844388, "z": 0, "heading": 0.5235987755982988 }
  },
  "topology": {
    "nodes": [
      { "id": "N1", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
      { "id": "N2", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] },
      { "id": "N3", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
      { "id": "N4", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] }
    ],
    "turnouts": [], "trackside": [],
    "edges": [
      { "id": "E1", "from": "N1.P1", "to": "N2.P1" },
      { "id": "E2", "from": "N3.P1", "to": "N4.P1" }
    ]
  },
  "geometry": { "turnouts": {}, "edges": {
    "E1": { "horizontal": [ { "kind": "straight", "length": 300.0 } ] },
    "E2": { "horizontal": [ { "kind": "arc", "radius": 100.0, "angle": -1.0471975511965976 } ] }
  } }
}`
	validateRejects(t, doc, "E1 x E2 в (0.0, 200.0)")
}

// TestValidateAxisRejectsOverlap — коллинеарное наложение: E2 лежит на линии
// E1 с 30-го по 100-й метр. Общая ось записана дважды.
func TestValidateAxisRejectsOverlap(t *testing.T) {
	doc := strings.Replace(twoEdgeMap(
		`{ "kind": "straight", "length": 100.0 }`,
		`{ "kind": "straight", "length": 100.0 }`),
		`"N3.P1": { "x": 50, "y": -50, "z": 0, "heading": 1.5707963267948966 }`,
		`"N3.P1": { "x": 30, "y": 0, "z": 0, "heading": 0 }`, 1)
	validateRejects(t, doc, "налагаются")
}

// TestValidateAxisRejectsSelfCrossing — цепочка одного элемента возвращается
// по себе: прямая на восток, дуга на π вниз, дуга на π обратно. Вторая дуга
// накладывается на первую (общая окружность), а её конец касается начала
// прямой в (100, 0).
func TestValidateAxisRejectsSelfCrossing(t *testing.T) {
	doc := strings.Replace(twoEdgeMap(
		`{ "kind": "straight", "length": 100.0 },
		  { "kind": "arc", "radius": 50.0, "angle": -3.141592653589793 },
		  { "kind": "arc", "radius": 50.0, "angle": -3.141592653589793 }`,
		`{ "kind": "straight", "length": 100.0 }`),
		`"N3.P1": { "x": 50, "y": -50, "z": 0, "heading": 1.5707963267948966 }`,
		`"N3.P1": { "x": 500, "y": 0, "z": 0, "heading": 0 }`, 1)
	validateRejects(t, doc, "E1")
}

// TestValidateAxisRejectsTwoCrossings — прямая пересекает полную окружность
// дважды: в (50, 0) и (250, 0). Обе точки обязаны попасть в отчёт.
func TestValidateAxisRejectsTwoCrossings(t *testing.T) {
	doc := strings.Replace(twoEdgeMap(
		`{ "kind": "straight", "length": 300.0 }`,
		`{ "kind": "arc", "radius": 100.0, "angle": 6.283185307179586 }`),
		`"N3.P1": { "x": 50, "y": -50, "z": 0, "heading": 1.5707963267948966 }`,
		`"N3.P1": { "x": 150, "y": -100, "z": 0, "heading": 0 }`, 1)
	err := validateErr(t, doc)
	for _, want := range []string{"в (50.0, 0.0)", "в (250.0, 0.0)"} {
		if !strings.Contains(err, want) {
			t.Fatalf("ожидалась точка %q, получено: %v", want, err)
		}
	}
}

func validateErr(t *testing.T, doc string) string {
	t.Helper()
	m, err := Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	err = Validate(m)
	if err == nil {
		t.Fatal("ожидался отказ, получен успех")
	}
	return err.Error()
}

// TestValidateAxisJointAllowed — два коллинеарных ребра, стыкующихся концами в
// общем порту: общая точка объяснена топологией и разрешена.
func TestValidateAxisJointAllowed(t *testing.T) {
	doc := `{
  "format_version": 4, "map_id": "J", "map_revision": 1,
  "anchors": { "N1.P1": { "x": 0, "y": 0, "z": 0, "heading": 0 } },
  "topology": {
    "nodes": [
      { "id": "N1", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
      { "id": "N2", "ports": [ { "id": "P1" } ] },
      { "id": "N3", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] }
    ],
    "turnouts": [], "trackside": [],
    "edges": [
      { "id": "E1", "from": "N1.P1", "to": "N2.P1" },
      { "id": "E2", "from": "N2.P1", "to": "N3.P1" }
    ]
  },
  "geometry": { "turnouts": {}, "edges": {
    "E1": { "horizontal": [ { "kind": "straight", "length": 100.0 } ] },
    "E2": { "horizontal": [ { "kind": "straight", "length": 100.0 } ] }
  } }
}`
	validateAccepts(t, doc)
}

// TestValidateAxisParallelTracksAllowed — два ребра между одними портами,
// оси которых не совпадают: параллельный путь законен, это не наложение.
func TestValidateAxisParallelTracksAllowed(t *testing.T) {
	doc := `{
  "format_version": 4, "map_id": "P", "map_revision": 1,
  "anchors": { "N1.P1": { "element": "E1", "x": 0, "y": 0, "z": 0, "heading": 0 } },
  "topology": {
    "nodes": [
      { "id": "N1", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
      { "id": "N2", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] }
    ],
    "turnouts": [], "trackside": [],
    "edges": [
      { "id": "E1", "from": "N1.P1", "to": "N2.P1" },
      { "id": "E2", "from": "N1.P1", "to": "N2.P1" }
    ]
  },
  "geometry": { "turnouts": {}, "edges": {
    "E1": { "horizontal": [ { "kind": "straight", "length": 100.0 } ] },
    "E2": { "horizontal": [ { "kind": "arc", "radius": 200.0, "angle": 0.5 } ] }
  } }
}`
	validateAccepts(t, doc)
}

// TestValidateFixtureStation — карта-фикстура обязана проходить валидацию,
// включая проверку пересечений: она эталон для тестов, которые её грузят.
func TestValidateFixtureStation(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "fixture_station.json"))
	if err != nil {
		t.Fatalf("карта: %v", err)
	}
	defer f.Close()
	m, err := Decode(f)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if err := Validate(m); err != nil {
		t.Fatalf("фикстура должна проходить валидацию: %v", err)
	}
}
