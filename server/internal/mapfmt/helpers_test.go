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
// # Почему у ФИКСТУР нет JSON, хотя у боевой карты он снова есть
//
// Карты тестов строятся кодом и портятся точечно через seedmap.Mutate. Порча,
// записанная кодом, называет себя в вызове, а фикстура, переставшая быть
// валидной, ловится тестом самой фабрики, а не чужим падением.
//
// Возврат файла карты 2026-08-12 этого не отменил и отменять не должен: три
// довода фабрики (шапка seedmap) относятся к фикстурам, а не к боевой карте.
// Единственный JSON, который читают тесты, — карта репозитория в shipped_test.go,
// и читают её именно затем, что она боевая.
package mapfmt_test

import (
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// rejects требует отказа валидатора с названной причиной. Пустая причина
// означает «любой отказ» и допустима только там, где текст отказа не является
// предметом теста.
func rejects(t *testing.T, m *mapfmt.Map, reason string) {
	t.Helper()
	err := mapfmt.Validate(m)
	if err == nil {
		t.Fatal("ожидался отказ, получен успех")
	}
	if reason != "" && !strings.Contains(err.Error(), reason) {
		t.Fatalf("ожидалась ошибка про %q, получено: %v", reason, err)
	}
}

// accepts требует, чтобы карта прошла валидацию целиком.
func accepts(t *testing.T, m *mapfmt.Map) {
	t.Helper()
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("карта должна быть валидна: %v", err)
	}
}

// refusal отдаёт текст отказа целиком — для случаев, где проверяется не одна
// подстрока, а несколько (например, обе точки пересечения).
func refusal(t *testing.T, m *mapfmt.Map) string {
	t.Helper()
	err := mapfmt.Validate(m)
	if err == nil {
		t.Fatal("ожидался отказ, получен успех")
	}
	return err.Error()
}

func straight(length float64) mapfmt.HPrim {
	return mapfmt.HPrim{Kind: "straight", Length: length}
}

func arc(radius, angle float64) mapfmt.HPrim {
	return mapfmt.HPrim{Kind: "arc", Radius: radius, Angle: angle}
}

// lineGeometry подменяет выравнивания единственного ребра перегона.
func lineGeometry(plan []mapfmt.HPrim, profile []mapfmt.VPrim) seedmap.Option {
	return seedmap.Mutate(func(m *mapfmt.Map) {
		m.Geometry.Edges[seedmap.LineEdgeID] = mapfmt.Alignments{Horizontal: plan, Vertical: profile}
	})
}

// twoComponents — карта из двух независимых рёбер, каждое со своим якорем:
// перегон фабрики (E1) плюс приставленное к нему второе ребро E2. Топология их
// не связывает, поэтому любая общая точка их осей — нарушение, кроме случаев,
// разобранных отдельными тестами.
//
// Рецепт решётки снят: покрытие рёбер run'ами здесь не предмет, а второе ребро
// его заведомо ломает.
func twoComponents(anchor1 mapfmt.Anchor, plan1 []mapfmt.HPrim, anchor2 mapfmt.Anchor, plan2 []mapfmt.HPrim) *mapfmt.Map {
	return seedmap.Line(seedmap.WithoutConstruction(), seedmap.Mutate(func(m *mapfmt.Map) {
		first := m.Topology.Edges[0]
		m.Topology.Nodes = append(m.Topology.Nodes,
			mapfmt.Node{ID: "NC", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
			mapfmt.Node{ID: "ND", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
		)
		m.Topology.Edges = append(m.Topology.Edges, mapfmt.Edge{ID: secondEdge, Kind: mapfmt.KindRail, From: "NC.P1", To: "ND.P1"})
		m.Geometry.Edges[seedmap.LineEdgeID] = mapfmt.Alignments{Horizontal: plan1}
		m.Geometry.Edges[secondEdge] = mapfmt.Alignments{Horizontal: plan2}
		m.Anchors = map[string]mapfmt.Anchor{first.From: anchor1, "NC.P1": anchor2}
	}))
}

// secondEdge — идентификатор ребра, приставляемого к перегону фабрики.
const secondEdge = "E2"
