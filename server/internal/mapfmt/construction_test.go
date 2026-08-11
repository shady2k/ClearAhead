package mapfmt

import (
	"strings"
	"testing"
)

// constructionMap — минимальная карта с блоком construction: два ребра E1 (100 м)
// и E2 (50 м), один тип, по одному run на ребро. Все остальные тесты модуля
// строятся мутацией этого документа.
const constructionMap = `{
  "format_version": 3,
  "map_id": "T",
  "map_revision": 1,
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
    "E2": { "horizontal": [ { "kind": "straight", "length": 50.0 } ] }
  } },
  "construction": {
    "default_type": "TRACK_MAIN",
    "types": [ {
      "id": "TRACK_MAIN",
      "gauge": 1.435,
      "sleeper": { "pitch": 0.6, "length": 2.5, "width": 0.28 },
      "ballast": { "half_width": 1.75 }
    } ],
    "runs": [
      { "id": "RUN_1", "coordinate": "u", "phase": 0.15,
        "spans": [ { "element": "E1", "from": 0, "to": 100, "direction": "forward" } ] },
      { "id": "RUN_2", "coordinate": "u", "phase": 0.0,
        "spans": [ { "element": "E2", "from": 0, "to": 50, "direction": "forward" } ] }
    ]
  }
}`

func loadConstruction(t *testing.T, doc string) *Map {
	t.Helper()
	m, err := Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	return m
}

func mustRejectConstruction(t *testing.T, doc, want string) {
	t.Helper()
	m := loadConstruction(t, doc)
	err := m.validateConstruction()
	if err == nil {
		t.Fatalf("ожидался отказ (%s), модуль прошёл", want)
	}
	if !strings.HasPrefix(err.Error(), "отрисовка: ") {
		t.Fatalf("отказ без префикса «отрисовка: »: %v", err)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("отказ %q не содержит %q", err, want)
	}
}

func TestConstructionValid(t *testing.T) {
	if err := loadConstruction(t, constructionMap).validateConstruction(); err != nil {
		t.Fatalf("валидная карта отвергнута: %v", err)
	}
}

func TestConstructionAbsent(t *testing.T) {
	// Блок construction — последний ключ документа: вырезаем его целиком,
	// остаётся карта без решётки.
	const block = `,
  "construction": {
    "default_type": "TRACK_MAIN",
    "types": [ {
      "id": "TRACK_MAIN",
      "gauge": 1.435,
      "sleeper": { "pitch": 0.6, "length": 2.5, "width": 0.28 },
      "ballast": { "half_width": 1.75 }
    } ],
    "runs": [
      { "id": "RUN_1", "coordinate": "u", "phase": 0.15,
        "spans": [ { "element": "E1", "from": 0, "to": 100, "direction": "forward" } ] },
      { "id": "RUN_2", "coordinate": "u", "phase": 0.0,
        "spans": [ { "element": "E2", "from": 0, "to": 50, "direction": "forward" } ] }
    ]
  }
}`
	doc := strings.Replace(constructionMap, block, "\n}", 1)
	m := loadConstruction(t, doc)
	if m.Construction != nil {
		t.Fatal("блок не должен был разобраться")
	}
	if err := m.validateConstruction(); err != nil {
		t.Fatalf("карта без блока отвергнута: %v", err)
	}
}

func TestConstructionTypeRanges(t *testing.T) {
	cases := []struct {
		name, from, to, want string
	}{
		{"gauge слишком широк", `"gauge": 1.435`, `"gauge": 10`, "gauge"},
		{"gauge слишком узок", `"gauge": 1.435`, `"gauge": 0.1`, "gauge"},
		{"шаг шпал вне диапазона", `"pitch": 0.6`, `"pitch": 0.001`, "sleeper.pitch"},
		{"шаг шпал слишком велик", `"pitch": 0.6`, `"pitch": 3.0`, "sleeper.pitch"},
		{"шпала слишком коротка", `"length": 2.5`, `"length": 0.5`, "sleeper.length"},
		{"шпала слишком узка", `"width": 0.28`, `"width": 0.01`, "sleeper.width"},
		{"балласт слишком узок", `"half_width": 1.75`, `"half_width": 0.1`, "ballast.half_width"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := strings.Replace(constructionMap, c.from, c.to, 1)
			mustRejectConstruction(t, doc, c.want)
		})
	}
}

func TestConstructionDuplicateTypeID(t *testing.T) {
	doc := strings.Replace(constructionMap,
		`"ballast": { "half_width": 1.75 }`,
		`"ballast": { "half_width": 1.75 } }, { "id": "TRACK_MAIN", "gauge": 1.435,
		  "sleeper": { "pitch": 0.6, "length": 2.5, "width": 0.28 },
		  "ballast": { "half_width": 1.75 }`, 1)
	mustRejectConstruction(t, doc, "объявлен дважды")
}

func TestConstructionDefaultType(t *testing.T) {
	doc := strings.Replace(constructionMap, `"default_type": "TRACK_MAIN"`, `"default_type": "NOPE"`, 1)
	mustRejectConstruction(t, doc, "тип по умолчанию")
}

func TestConstructionRunType(t *testing.T) {
	doc := strings.Replace(constructionMap, `"id": "RUN_1"`, `"id": "RUN_1", "type": "NOPE"`, 1)
	mustRejectConstruction(t, doc, "неизвестный тип")
}

func TestConstructionRunCoordinate(t *testing.T) {
	doc := strings.Replace(constructionMap, `"coordinate": "u"`, `"coordinate": "s"`, 1)
	mustRejectConstruction(t, doc, "coordinate")
}

func TestConstructionRunPhase(t *testing.T) {
	for _, c := range []struct{ name, from, to, want string }{
		{"фаза равна шагу", `"phase": 0.15`, `"phase": 0.6`, "phase"},
		{"фаза больше шага", `"phase": 0.15`, `"phase": 0.7`, "phase"},
		{"фаза отрицательна", `"phase": 0.15`, `"phase": -0.1`, "phase"},
	} {
		t.Run(c.name, func(t *testing.T) {
			doc := strings.Replace(constructionMap, c.from, c.to, 1)
			mustRejectConstruction(t, doc, c.want)
		})
	}
}

func TestConstructionRunNoSpans(t *testing.T) {
	doc := strings.Replace(constructionMap,
		`"spans": [ { "element": "E1", "from": 0, "to": 100, "direction": "forward" } ]`,
		`"spans": []`, 1)
	mustRejectConstruction(t, doc, "пустая протяжённость")
}

func TestConstructionSpanDirection(t *testing.T) {
	doc := strings.Replace(constructionMap, `"direction": "forward"`, `"direction": "left"`, 1)
	mustRejectConstruction(t, doc, "направление")
}

func TestConstructionSpanUnknownElement(t *testing.T) {
	doc := strings.Replace(constructionMap, `"element": "E1"`, `"element": "E9"`, 1)
	mustRejectConstruction(t, doc, "не существует")
}

func TestConstructionSpanDegenerate(t *testing.T) {
	doc := strings.Replace(constructionMap, `"from": 0, "to": 100`, `"from": 40, "to": 40`, 1)
	mustRejectConstruction(t, doc, "вырожден")
}

func TestConstructionSpanBeyondDomain(t *testing.T) {
	doc := strings.Replace(constructionMap, `"from": 0, "to": 100`, `"from": 0, "to": 100.001`, 1)
	mustRejectConstruction(t, doc, "за пределами")
}

func TestConstructionUncoveredEdge(t *testing.T) {
	// Остаётся только RUN_1: E2 не покрыт ни одним run.
	doc := strings.Replace(constructionMap,
		`      { "id": "RUN_1", "coordinate": "u", "phase": 0.15,
        "spans": [ { "element": "E1", "from": 0, "to": 100, "direction": "forward" } ] },
      { "id": "RUN_2", "coordinate": "u", "phase": 0.0,
        "spans": [ { "element": "E2", "from": 0, "to": 50, "direction": "forward" } ] }`,
		`      { "id": "RUN_1", "coordinate": "u", "phase": 0.15,
        "spans": [ { "element": "E1", "from": 0, "to": 100, "direction": "forward" } ] }`, 1)
	mustRejectConstruction(t, doc, "не покрыто ни одним run")
}

func TestConstructionEdgeCoveredTwice(t *testing.T) {
	doc := strings.Replace(constructionMap,
		`"id": "RUN_2", "coordinate": "u", "phase": 0.0,
        "spans": [ { "element": "E2"`,
		`"id": "RUN_2", "coordinate": "u", "phase": 0.0,
        "spans": [ { "element": "E1"`, 1)
	mustRejectConstruction(t, doc, "ровно один run")
}

func TestConstructionCoverageGap(t *testing.T) {
	// E1 разбит на два спана с пропуском 49–50.
	doc := strings.Replace(constructionMap,
		`"spans": [ { "element": "E1", "from": 0, "to": 100, "direction": "forward" } ]`,
		`"spans": [
		  { "element": "E1", "from": 0, "to": 49, "direction": "forward" },
		  { "element": "E1", "from": 50, "to": 100, "direction": "forward" } ]`, 1)
	mustRejectConstruction(t, doc, "пропуск")
}

func TestConstructionCoverageOverlap(t *testing.T) {
	// Два спана на E1 в одном run'е с перекрытием.
	doc := strings.Replace(constructionMap,
		`"spans": [ { "element": "E1", "from": 0, "to": 100, "direction": "forward" } ]`,
		`"spans": [
		  { "element": "E1", "from": 0, "to": 60, "direction": "forward" },
		  { "element": "E1", "from": 50, "to": 100, "direction": "forward" } ]`, 1)
	mustRejectConstruction(t, doc, "перекрытие")
}

func TestConstructionCoverageStartNotZero(t *testing.T) {
	doc := strings.Replace(constructionMap, `"from": 0, "to": 100`, `"from": 1, "to": 100`, 1)
	mustRejectConstruction(t, doc, "начинается с")
}

func TestConstructionCoverageEndShort(t *testing.T) {
	doc := strings.Replace(constructionMap, `"from": 0, "to": 100`, `"from": 0, "to": 99.5`, 1)
	// Пропуск 99.5–100 — «кончается на» или «пропуск»: оба отказа.
	m := loadConstruction(t, doc)
	if err := m.validateConstruction(); err == nil {
		t.Fatal("ожидался отказ по неполному покрытию")
	}
}

// constructionTurnoutMap — карта со стрелкой: проходы устройства run'ами не
// покрываются, а сами устройства несут собственный type (спека §4).
const constructionTurnoutMap = `{
  "format_version": 3,
  "map_id": "T2",
  "map_revision": 1,
  "anchors": { "NW.P1": { "x": 0, "y": 0, "z": 0, "heading": 0 } },
  "topology": {
    "nodes": [
      { "id": "NW", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
      { "id": "NS", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] },
      { "id": "ND", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] }
    ],
    "turnouts": [
      { "id": "SW1", "hand": "right", "frog": "1/9", "type": "TRACK_MAIN",
        "ports": { "common": "C", "straight": "S", "diverging": "D" } }
    ],
    "trackside": [],
    "edges": [
      { "id": "EA", "from": "NW.P1", "to": "SW1.C" },
      { "id": "ES", "from": "SW1.S", "to": "NS.P1" },
      { "id": "ED", "from": "SW1.D", "to": "ND.P1" }
    ]
  },
  "geometry": { "turnouts": {
      "SW1": {
        "straight":  { "horizontal": [ { "kind": "straight", "length": 33.5 } ] },
        "diverging": { "horizontal": [ { "kind": "arc", "radius": 300.0, "angle": -0.1107 } ] }
      }
    },
    "edges": {
      "EA": { "horizontal": [ { "kind": "straight", "length": 100.0 } ] },
      "ES": { "horizontal": [ { "kind": "straight", "length": 200.0 } ] },
      "ED": { "horizontal": [ { "kind": "straight", "length": 200.0 } ] }
    }
  },
  "construction": {
    "default_type": "TRACK_MAIN",
    "types": [ {
      "id": "TRACK_MAIN",
      "gauge": 1.435,
      "sleeper": { "pitch": 0.6, "length": 2.5, "width": 0.28 },
      "ballast": { "half_width": 1.75 }
    } ],
    "runs": [
      { "id": "RUN_A", "coordinate": "u", "phase": 0.0,
        "spans": [ { "element": "EA", "from": 0, "to": 100, "direction": "forward" } ] },
      { "id": "RUN_S", "coordinate": "u", "phase": 0.0,
        "spans": [ { "element": "ES", "from": 0, "to": 200, "direction": "forward" } ] },
      { "id": "RUN_D", "coordinate": "u", "phase": 0.0,
        "spans": [ { "element": "ED", "from": 0, "to": 200, "direction": "forward" } ] }
    ]
  }
}`

func TestConstructionTurnoutValid(t *testing.T) {
	if err := loadConstruction(t, constructionTurnoutMap).validateConstruction(); err != nil {
		t.Fatalf("карта со стрелкой отвергнута: %v", err)
	}
}

// TestConstructionRunOnPassage — спана на проходе устройства: решётка устройств
// нерегулярна, run'ы её не покрывают.
func TestConstructionRunOnPassage(t *testing.T) {
	doc := strings.Replace(constructionTurnoutMap,
		`"spans": [ { "element": "EA", "from": 0, "to": 100, "direction": "forward" } ]`,
		`"spans": [ { "element": "SW1:straight", "from": 0, "to": 33.5, "direction": "forward" } ]`, 1)
	// Проход покрыт, а ребро EA — нет: отказ обязан прийти по проходу.
	mustRejectConstruction(t, doc, "проход устройства")
}

func TestConstructionUnknownTurnoutType(t *testing.T) {
	doc := strings.Replace(constructionTurnoutMap, `"type": "TRACK_MAIN"`, `"type": "NOPE"`, 1)
	mustRejectConstruction(t, doc, "стрелка")
}

// platformSizesDoc — constructionMap с платформой PLAT на ребре E2. Размеры
// вставляются строкой fields: каждый тест мутирует одно поле.
func platformSizesDoc(fields string) string {
	return strings.Replace(constructionMap,
		`"turnouts": [], "trackside": [],`,
		`"turnouts": [], "trackside": [ { "id": "PLAT", "kind": "platform", `+fields+`
		  "span": [ { "element": "E2", "from": 0, "to": 50 } ] } ],`, 1)
}

// withoutConstruction вырезает блок construction — последний ключ документа.
func withoutConstruction(doc string) string {
	i := strings.Index(doc, `,
  "construction": {`)
	if i < 0 {
		panic("в документе нет блока construction")
	}
	return doc[:i] + "\n}"
}

func TestConstructionPlatformSizesValid(t *testing.T) {
	doc := platformSizesDoc(`"side": "right", "offset": 1.75, "width": 3.0,`)
	if err := loadConstruction(t, doc).validateConstruction(); err != nil {
		t.Fatalf("платформа с размерами отвергнута: %v", err)
	}
}

// TestConstructionPlatformSizes — размеры обязательны и в правдоподобных
// пределах (спека §3). Пропущенное поле — ноль, а ноль вне диапазона: отказ
// приходит по той же формуле !(v >= min && v <= max), что и у типа (§3), —
// валидатор не обязан полагаться на checkFinite.
func TestConstructionPlatformSizes(t *testing.T) {
	cases := []struct {
		name, fields, want string
	}{
		{"offset пропущен", `"side": "right", "width": 3.0,`, "offset"},
		{"offset слишком мал", `"side": "right", "offset": 0.5, "width": 3.0,`, "offset"},
		{"offset слишком велик", `"side": "right", "offset": 10.0, "width": 3.0,`, "offset"},
		{"width пропущена", `"side": "right", "offset": 1.75,`, "width"},
		{"width слишком мала", `"side": "right", "offset": 1.75, "width": 0.3,`, "width"},
		{"width слишком велика", `"side": "right", "offset": 1.75, "width": 30.0,`, "width"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustRejectConstruction(t, platformSizesDoc(c.fields), c.want)
		})
	}
}

// TestConstructionPlatformSizesWithoutBlock — размеры платформы проверяются и
// на карте без блока construction: они часть контракта отрисовки, а не блока
// рецепта, и карта с голой платформой не должна выйти наружу.
func TestConstructionPlatformSizesWithoutBlock(t *testing.T) {
	// Без размеров — отказ, хотя блока construction нет.
	doc := withoutConstruction(platformSizesDoc(`"side": "right",`))
	m := loadConstruction(t, doc)
	if m.Construction != nil {
		t.Fatal("блок не должен был разобраться")
	}
	err := m.validateConstruction()
	if err == nil {
		t.Fatal("платформа без размеров прошла, ожидался отказ")
	}
	if !strings.Contains(err.Error(), "offset") {
		t.Fatalf("отказ по не той причине: %v", err)
	}
	// С размерами — проходит.
	doc = withoutConstruction(platformSizesDoc(`"side": "right", "offset": 1.75, "width": 3.0,`))
	if err := loadConstruction(t, doc).validateConstruction(); err != nil {
		t.Fatalf("карта без блока, но с размерами платформы отвергнута: %v", err)
	}
}

// TestConstructionBufferStopNoSizes — buffer_stop точечный, размеров не несёт.
func TestConstructionBufferStopNoSizes(t *testing.T) {
	doc := strings.Replace(constructionMap,
		`"turnouts": [], "trackside": [],`,
		`"turnouts": [], "trackside": [ { "id": "BS", "kind": "buffer_stop",
		  "span": [ { "element": "E2", "from": 25, "to": 25 } ] } ],`, 1)
	if err := loadConstruction(t, doc).validateConstruction(); err != nil {
		t.Fatalf("buffer_stop без размеров отвергнут: %v", err)
	}
}
