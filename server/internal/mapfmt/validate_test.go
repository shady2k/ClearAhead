package mapfmt

import (
	"strings"
	"testing"
)

func decodeOK(t *testing.T, doc string) *Map {
	t.Helper()
	m, err := Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	return m
}

func TestValidateMinimal(t *testing.T) {
	if err := Validate(decodeOK(t, minimalMap)); err != nil {
		t.Fatalf("минимальная карта должна быть валидна: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct{ name, doc, want string }{
		{
			"домены не совпадают",
			strings.Replace(minimalMap,
				`"horizontal": [ { "kind": "straight", "length": 100.0 } ]`,
				`"horizontal": [ { "kind": "straight", "length": 100.0 } ],
				 "vertical": [ { "kind": "grade", "length": 90.0, "slope_permille": 0 } ]`, 1),
			"домен",
		},
		{
			"вертикаль начинается не с grade",
			strings.Replace(minimalMap,
				`"horizontal": [ { "kind": "straight", "length": 100.0 } ]`,
				`"horizontal": [ { "kind": "straight", "length": 100.0 } ],
				 "vertical": [ { "kind": "vertical_curve", "length": 100.0, "end_slope_permille": 5 } ]`, 1),
			"grade",
		},
		{
			"нулевая длина",
			strings.Replace(minimalMap, `"length": 100.0`, `"length": 0.0`, 1),
			"длина",
		},
		{
			"неизвестный kind",
			strings.Replace(minimalMap, `"kind": "straight"`, `"kind": "spiral"`, 1),
			"неизвестный",
		},
		{
			"висящее ребро без назначения",
			strings.Replace(minimalMap, `"purpose": "buffer_stop"`, `"purpose": ""`, 1),
			"висящ",
		},
		{
			"путевой объект на несуществующем элементе",
			strings.Replace(minimalMap,
				`"turnouts": [], "trackside": [],`,
				`"turnouts": [],
				 "trackside": [ { "id": "TS1", "kind": "platform", "span": [ { "element": "E9", "from": 0, "to": 10 } ] } ],`, 1),
			"несуществующий элемент",
		},
		{
			"геопривязка с недопустимым origin_height_kind",
			strings.Replace(minimalMap,
				`"map_revision": 1,`,
				`"map_revision": 1,
				 "georeference": { "datum": "WGS84", "origin_height_kind": "geoid" },`, 1),
			"origin_height_kind",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := Decode(strings.NewReader(c.doc))
			if err != nil {
				if !strings.Contains(err.Error(), c.want) {
					t.Fatalf("отказ на разборе не про то: %v", err)
				}
				return
			}
			err = Validate(m)
			if err == nil {
				t.Fatal("ожидался отказ, получен успех")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ожидалась ошибка про %q, получено: %v", c.want, err)
			}
		})
	}
}

// turnoutMap — горловина из примера спеки §11 в миниатюре: вход, стрелка,
// два пути. Все три порта стрелки соединены, концы путей объявлены.
const turnoutMap = `{
  "format_version": 2,
  "map_id": "T",
  "map_revision": 1,
  "anchors": { "N1.P1": { "x": 0, "y": 0, "z": 0, "heading": 0 } },
  "topology": {
    "nodes": [
      { "id": "N1", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
      { "id": "N2", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] },
      { "id": "N3", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] }
    ],
    "turnouts": [
      { "id": "T1", "hand": "left", "frog": "1/9", "ports": { "common": "C", "straight": "S", "diverging": "D" } }
    ],
    "trackside": [],
    "edges": [
      { "id": "E1", "from": "N1.P1", "to": "T1.C" },
      { "id": "E2", "from": "T1.S", "to": "N2.P1" },
      { "id": "E3", "from": "T1.D", "to": "N3.P1" }
    ]
  },
  "geometry": {
    "turnouts": {
      "T1": {
        "straight": { "horizontal": [ { "kind": "straight", "length": 33.5 } ] },
        "diverging": { "horizontal": [ { "kind": "arc", "radius": 300.0, "angle": -0.1107 } ] }
      }
    },
    "edges": {
      "E1": { "horizontal": [ { "kind": "straight", "length": 300.0 } ] },
      "E2": { "horizontal": [ { "kind": "straight", "length": 400.0 } ] },
      "E3": { "horizontal": [ { "kind": "straight", "length": 400.0 } ] }
    }
  }
}`

func TestValidateTurnoutConnected(t *testing.T) {
	// Порт стрелки с одним ребром законен — продолжение даёт сама стрелка.
	if err := Validate(decodeOK(t, turnoutMap)); err != nil {
		t.Fatalf("карта со стрелкой должна быть валидна: %v", err)
	}
}

func TestValidateTurnoutPortUnconnected(t *testing.T) {
	// Отрезаем ребро E3 и его геометрию: порт T1.D остаётся вообще без ребра —
	// это отказ, а не висящий конец с просьбой про purpose.
	doc := strings.Replace(turnoutMap,
		`      { "id": "E2", "from": "T1.S", "to": "N2.P1" },
      { "id": "E3", "from": "T1.D", "to": "N3.P1" }
`,
		`      { "id": "E2", "from": "T1.S", "to": "N2.P1" }
`, 1)
	doc = strings.Replace(doc,
		`      "E2": { "horizontal": [ { "kind": "straight", "length": 400.0 } ] },
      "E3": { "horizontal": [ { "kind": "straight", "length": 400.0 } ] }
`,
		`      "E2": { "horizontal": [ { "kind": "straight", "length": 400.0 } ] }
`, 1)
	m, err := Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	err = Validate(m)
	if err == nil {
		t.Fatal("ожидался отказ, получен успех")
	}
	if !strings.Contains(err.Error(), "не соединён") {
		t.Fatalf("ожидалась ошибка про не соединённый порт стрелки, получено: %v", err)
	}
}

func TestTurnoutWithoutFrogIsValid(t *testing.T) {
	m := loadTestMap(t, "testdata/turnout_no_frog.json")
	if err := Validate(m); err != nil {
		t.Fatalf("марка крестовины необязательна (§8), получен отказ: %v", err)
	}
}
