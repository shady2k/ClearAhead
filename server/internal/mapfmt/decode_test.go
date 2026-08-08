package mapfmt

import (
	"strings"
	"testing"
)

const minimalMap = `{
  "format_version": 1,
  "map_id": "T",
  "map_revision": 1,
  "anchors": { "N1.P1": { "x": 0, "y": 0, "z": 0, "heading": 0 } },
  "topology": {
    "nodes": [
      { "id": "N1", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
      { "id": "N2", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] }
    ],
    "turnouts": [], "trackside": [],
    "edges": [ { "id": "E1", "from": "N1.P1", "to": "N2.P1" } ]
  },
  "geometry": {
    "turnouts": {},
    "edges": { "E1": { "horizontal": [ { "kind": "straight", "length": 100.0 } ] } }
  }
}`

func TestDecodeMinimal(t *testing.T) {
	m, err := Decode(strings.NewReader(minimalMap))
	if err != nil {
		t.Fatalf("разбор минимальной карты: %v", err)
	}
	if m.MapID != "T" || len(m.Topology.Edges) != 1 {
		t.Fatalf("разобрано неверно: %+v", m)
	}
}

func TestDecodeRejects(t *testing.T) {
	cases := []struct{ name, doc, want string }{
		{"дубликат ключа", `{"map_id":"A","map_id":"B"}`, "дублирующийся ключ"},
		// Дубликат после вложенного объекта: контейнер — тоже значение, и
		// pending родителя обязан сброситься, иначе ключ принимается за значение.
		{"дубликат ключа после вложенного объекта", `{"a":{"b":1},"a":2}`, "дублирующийся ключ"},
		{"не объект", `"x"`, "объект"},
		{"неизвестное поле", `{"format_version":1,"nope":1}`, "неизвестн"},
		{"не число", `{"format_version":1,"map_revision":1,"map_id":"T",
			"anchors":{"N1.P1":{"x":1e400,"y":0,"z":0,"heading":0}},
			"topology":{"nodes":[],"turnouts":[],"edges":[],"trackside":[]},
			"geometry":{"turnouts":{},"edges":{}}}`, ""},
		{"чужая версия", `{"format_version":2}`, "версия"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(c.doc)); err == nil {
				t.Fatal("ожидался отказ, получен успех")
			} else if c.want != "" && !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ожидалась ошибка про %q, получено: %v", c.want, err)
			}
		})
	}
}

func TestDecodeDepthLimit(t *testing.T) {
	doc := strings.Repeat(`{"a":`, MaxNestingDepth+2) + `1` + strings.Repeat(`}`, MaxNestingDepth+2)
	if _, err := Decode(strings.NewReader(doc)); err == nil {
		t.Fatal("ожидался отказ по глубине")
	}
}
