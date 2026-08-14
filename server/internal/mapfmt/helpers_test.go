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
// перегон фабрики (E1) плюс приставленное к нему второе ребро (метка E2).
// Топология их не связывает, поэтому любая общая точка их осей — нарушение,
// кроме случаев, разобранных отдельными тестами.
//
// Рецепт решётки снят: покрытие рёбер run'ами здесь не предмет, а второе ребро
// его заведомо ломает.
func twoComponents(anchor1 mapfmt.Anchor, plan1 []mapfmt.HPrim, anchor2 mapfmt.Anchor, plan2 []mapfmt.HPrim) *mapfmt.Map {
	return seedmap.Line(seedmap.WithoutConstruction(), seedmap.Mutate(func(m *mapfmt.Map) {
		first := m.Topology.Edges[0]
		m.Topology.Nodes = append(m.Topology.Nodes,
			mapfmt.Node{ID: tID03, Name: "NC", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
			mapfmt.Node{ID: tID04, Name: "ND", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
		)
		m.Topology.Edges = append(m.Topology.Edges, mapfmt.Edge{ID: secondEdge, Name: "E2", Kind: mapfmt.KindRail, From: tID03 + ".P1", To: tID04 + ".P1"})
		m.Geometry.Edges[seedmap.LineEdgeID] = mapfmt.Alignments{Horizontal: plan1}
		m.Geometry.Edges[secondEdge] = mapfmt.Alignments{Horizontal: plan2}
		m.Anchors = map[string]mapfmt.Anchor{first.From: anchor1, tID03 + ".P1": anchor2}
	}))
}

// secondEdge — идентификатор ребра (метка E2), приставляемого к перегону
// фабрики. UUID из таблицы ниже: два одинаковых UUID в одной карте дали бы
// отказ не по предмету теста.
const secondEdge = tID05

// Тестовые идентификаторы фикстур — UUIDv7 из фиксированной таблицы (решение
// владельца 2026-08-13 «UUIDv7 везде»): тождество элемента — UUID, читаемая
// метка — отдельное поле name. Значения детерминированы и не пересекаются с
// таблицей seedmap: тесты собирают карты поверх фикстур фабрики, и два
// одинаковых UUID в одной карте дали бы отказ по поводу, не относящемуся к
// предмету теста.
const (
	tID00 = "01a3185c-5001-7242-8242-000000424242" // метка N1
	tID01 = "01a3185c-5002-7242-8242-000001424242" // метка N2
	tID02 = "01a3185c-5003-7242-8242-000002424242" // метка N3
	tID03 = "01a3185c-5004-7242-8242-000003424242" // метка NC
	tID04 = "01a3185c-5005-7242-8242-000004424242" // метка ND
	tID05 = "01a3185c-5006-7242-8242-000005424242" // метка E2
	tID06 = "01a3185c-5007-7242-8242-000006424242" // метка SW
	tID07 = "01a3185c-5008-7242-8242-000007424242" // метка TS1
	tID08 = "01a3185c-5009-7242-8242-000008424242" // метка RIV_1
	tID09 = "01a3185c-500a-7242-8242-000009424242" // метка T
	tID10 = "01a3185c-500b-7242-8242-00000a424242" // метка BLD_1
	tID11 = "01a3185c-500c-7242-8242-00000b424242" // метка RUN_X
	tID12 = "01a3185c-500d-7242-8242-00000c424242" // метка TYPE_X
	tID13 = "01a3185c-500e-7242-8242-00000d424242" // метка BS
	tID14 = "01a3185c-500f-7242-8242-00000e424242" // метка BS_X
	tID15 = "01a3185c-5010-7242-8242-00000f424242" // метка EA
	tID16 = "01a3185c-5011-7242-8242-000010424242" // метка EB
	tID17 = "01a3185c-5012-7242-8242-000011424242" // метка EC
	tID18 = "01a3185c-5013-7242-8242-000012424242" // метка ED
	tID19 = "01a3185c-5014-7242-8242-000013424242" // метка EE
	tID20 = "01a3185c-5015-7242-8242-000014424242" // метка EF
	tID21 = "01a3185c-5016-7242-8242-000015424242" // метка SW1
	tID22 = "01a3185c-5017-7242-8242-000016424242" // метка SW2
)
