// Общие построители и утверждения тестов mapfmt.
//
// # Почему внешний пакет
//
// Тесты пакета лежат в mapfmt_test, а не в mapfmt, и это не вкус, а
// необходимость: карты строит фабрика seedmap, а она импортирует mapfmt —
// тест ВНУТРИ пакета замкнул бы импорт в цикл. Побочная выгода: тесты видят
// только экспортированный контракт (Decode, Validate), то есть проверяют то
// же, что видит остальной сервер.
//
// # Почему нет JSON
//
// Карты строятся кодом и портятся точечно через seedmap.Mutate. Порча,
// записанная кодом, называет себя в вызове, а фикстура, переставшая быть
// валидной, ловится тестом самой фабрики, а не чужим падением.
package mapfmt_test

import (
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// отвергает требует отказа валидатора с названной причиной. Пустая причина
// означает «любой отказ» и допустима только там, где текст отказа не является
// предметом теста.
func отвергает(t *testing.T, m *mapfmt.Map, причина string) {
	t.Helper()
	err := mapfmt.Validate(m)
	if err == nil {
		t.Fatal("ожидался отказ, получен успех")
	}
	if причина != "" && !strings.Contains(err.Error(), причина) {
		t.Fatalf("ожидалась ошибка про %q, получено: %v", причина, err)
	}
}

// принимает требует, чтобы карта прошла валидацию целиком.
func принимает(t *testing.T, m *mapfmt.Map) {
	t.Helper()
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("карта должна быть валидна: %v", err)
	}
}

// отказ отдаёт текст отказа целиком — для случаев, где проверяется не одна
// подстрока, а несколько (например, обе точки пересечения).
func отказ(t *testing.T, m *mapfmt.Map) string {
	t.Helper()
	err := mapfmt.Validate(m)
	if err == nil {
		t.Fatal("ожидался отказ, получен успех")
	}
	return err.Error()
}

func прямая(длина float64) mapfmt.HPrim {
	return mapfmt.HPrim{Kind: "straight", Length: длина}
}

func дуга(радиус, угол float64) mapfmt.HPrim {
	return mapfmt.HPrim{Kind: "arc", Radius: радиус, Angle: угол}
}

// геометрияПерегона подменяет выравнивания единственного ребра перегона.
func геометрияПерегона(план []mapfmt.HPrim, профиль []mapfmt.VPrim) seedmap.Option {
	return seedmap.Mutate(func(m *mapfmt.Map) {
		m.Geometry.Edges[seedmap.LineEdgeID] = mapfmt.Alignments{Horizontal: план, Vertical: профиль}
	})
}

// двеКомпоненты — карта из двух независимых рёбер, каждое со своим якорем:
// перегон фабрики (E1) плюс приставленное к нему второе ребро E2. Топология их
// не связывает, поэтому любая общая точка их осей — нарушение, кроме случаев,
// разобранных отдельными тестами.
//
// Рецепт решётки снят: покрытие рёбер run'ами здесь не предмет, а второе ребро
// его заведомо ломает.
func двеКомпоненты(якорь1 mapfmt.Anchor, план1 []mapfmt.HPrim, якорь2 mapfmt.Anchor, план2 []mapfmt.HPrim) *mapfmt.Map {
	return seedmap.Line(seedmap.WithoutConstruction(), seedmap.Mutate(func(m *mapfmt.Map) {
		первое := m.Topology.Edges[0]
		m.Topology.Nodes = append(m.Topology.Nodes,
			mapfmt.Node{ID: "NC", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
			mapfmt.Node{ID: "ND", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
		)
		m.Topology.Edges = append(m.Topology.Edges, mapfmt.Edge{ID: второеРебро, From: "NC.P1", To: "ND.P1"})
		m.Geometry.Edges[seedmap.LineEdgeID] = mapfmt.Alignments{Horizontal: план1}
		m.Geometry.Edges[второеРебро] = mapfmt.Alignments{Horizontal: план2}
		m.Anchors = map[string]mapfmt.Anchor{первое.From: якорь1, "NC.P1": якорь2}
	}))
}

// второеРебро — идентификатор ребра, приставляемого к перегону фабрики.
const второеРебро = "E2"
