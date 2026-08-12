package mapfmt_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// minimalMap — самая маленькая карта, которую разбор обязан принять. Валидатор
// её не смотрит: разбор проверяет ФОРМУ, а не смысл, и тест про форму не
// должен падать от правил связности.
const minimalMap = `{
  "format_version": 6,
  "map_id": "T",
  "map_revision": 1,
  "anchors": { "N1.P1": { "x": 0, "y": 0, "z": 0, "heading": 0 } },
  "topology": {
    "nodes": [
      { "id": "N1", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
      { "id": "N2", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] }
    ],
    "turnouts": [], "structures": [],
    "edges": [ { "id": "E1", "kind": "rail", "from": "N1.P1", "to": "N2.P1" } ]
  },
  "geometry": {
    "turnouts": {},
    "edges": { "E1": { "horizontal": [ { "kind": "straight", "length": 100.0 } ] } }
  }
}`

func TestDecodeMinimal(t *testing.T) {
	m, err := mapfmt.Decode(strings.NewReader(minimalMap))
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
		{"неизвестное поле", `{"format_version": 6,"nope":1}`, "неизвестн"},
		{"не число", `{"format_version": 6,"map_revision":1,"map_id":"T",
			"anchors":{"N1.P1":{"x":1e400,"y":0,"z":0,"heading":0}},
			"topology":{"nodes":[],"turnouts":[],"edges":[],"structures":[]},
			"geometry":{"turnouts":{},"edges":{}}}`, ""},
		// Версия заведомо чужая с обеих сторон: 1 больше не поддерживается,
		// 99 не наступит.
		{"устаревшая версия", `{"format_version":1}`, "версия"},
		{"будущая версия", `{"format_version":99}`, "версия"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := mapfmt.Decode(strings.NewReader(c.doc)); err == nil {
				t.Fatal("ожидался отказ, получен успех")
			} else if c.want != "" && !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ожидалась ошибка про %q, получено: %v", c.want, err)
			}
		})
	}
}

// Опечатка в имени поля обязана быть НАЗВАНА, а не проглочена.
//
// Это главный случай всего разбора и потому отдельный тест, а не строка в
// таблице выше. encoding/json по умолчанию молча выбрасывает незнакомое поле:
// карта с `half_widht` вместо `half_width` загрузилась бы, а река получила бы
// нулевую полуширину — то есть правдоподобную подстановку вместо отказа.
// Проверяется и то, что отказ произошёл, и то, что в тексте есть само имя: без
// имени автор карты ищет опечатку глазами по восьмистам строкам.
func TestDecodeNamesUnknownField(t *testing.T) {
	doc := strings.Replace(minimalMap, `"map_revision": 1`, `"map_revisoin": 1`, 1)
	_, err := mapfmt.Decode(strings.NewReader(doc))
	if err == nil {
		t.Fatal("карта с опечаткой в имени поля принята")
	}
	if !strings.Contains(err.Error(), "map_revisoin") {
		t.Fatalf("отказ не называет поле: %v", err)
	}
}

// Опечатка ВГЛУБИНЕ документа ловится так же, как в корне: строгость не
// заканчивается на первом уровне вложенности.
func TestDecodeNamesUnknownFieldInNestedObject(t *testing.T) {
	doc := strings.Replace(minimalMap, `"purpose": "buffer_stop"`, `"porpose": "buffer_stop"`, 1)
	_, err := mapfmt.Decode(strings.NewReader(doc))
	if err == nil {
		t.Fatal("вложенное неизвестное поле принято")
	}
	if !strings.Contains(err.Error(), "porpose") {
		t.Fatalf("отказ не называет поле: %v", err)
	}
}

func TestDecodeDepthLimit(t *testing.T) {
	doc := strings.Repeat(`{"a":`, mapfmt.MaxNestingDepth+2) + `1` + strings.Repeat(`}`, mapfmt.MaxNestingDepth+2)
	if _, err := mapfmt.Decode(strings.NewReader(doc)); err == nil {
		t.Fatal("ожидался отказ по глубине")
	}
}

// Отсутствующий файл — отказ, называющий путь. Проверяется потому, что именно
// этим отказом сервер обязан упасть на старте, если карты нет: тихой подстановки
// затравки из кода больше не существует.
func TestDecodeFileNamesMissingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "нет-такой-карты.json")
	_, err := mapfmt.DecodeFile(path)
	if err == nil {
		t.Fatal("отсутствующий файл разобран")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("отказ не называет путь: %v", err)
	}
}
