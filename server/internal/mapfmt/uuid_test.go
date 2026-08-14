package mapfmt_test

import (
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// Тесты тождества по решению владельца 2026-08-13 «UUIDv7 везде»: тождество
// элемента — UUIDv7, и только оно; метка (name) — отдельное необязательное
// поле, тождеством не является. Ссылки идут по UUID, порты остаются слотами.

// Не-UUID на месте тождества отвергается с названием пришедшего значения:
// «не UUIDv7: "E1"» — по одному «неверный id» автор не найдёт опечатку.
func TestNonUUIDIDIsRejected(t *testing.T) {
	m := seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) {
		m.Topology.Edges[0].ID = "E1"
	}))
	rejects(t, m, `не UUIDv7: "E1"`)
}

// Строка в форме UUIDv4 отвергается по биту версии, а не по форме: 36
// символов, дефисы на местах, строчная шестнадцатеричная запись и вариант
// RFC 4122 — все проверки формы пройдены, седьмым её не делает только бит
// версии. Принять UUIDv4 под видом седьмого — потерять временной порядок,
// ради которого версия и выбрана.
func TestUUIDv4DisguisedAsV7IsRejected(t *testing.T) {
	v4 := "01a3185c-5001-4242-8242-000001424242"
	m := seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) {
		m.Topology.Edges[0].ID = v4
	}))
	rejects(t, m, "версия 4, ожидалась 7")
}

// Две одинаковые метки законны: метка — читаемое имя, а не адрес, и повтор
// имени не делает адресацию неоднозначной, пока тождества (UUID) различны.
// Перегон Corridor несёт два ребра с метками E1 и E2; вторая переименована
// в E1 — карта обязана остаться валидной целиком.
func TestDuplicateNamesAreLegal(t *testing.T) {
	m := seedmap.Corridor(seedmap.Mutate(func(m *mapfmt.Map) {
		m.Topology.Edges[1].Name = m.Topology.Edges[0].Name
	}))
	accepts(t, m)
}

// Два одинаковых UUID в одной карте — отказ: UUID живёт в одном пространстве
// имён карты, и элемент второго класса с тем же UUID делает адресацию
// неоднозначной. Отказ приходит от сквозной проверки уникальности и называет
// обе метки рядом с UUID.
func TestDuplicateUUIDIsRejected(t *testing.T) {
	m := seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) {
		m.Topology.Nodes = append(m.Topology.Nodes, mapfmt.Node{
			ID:    seedmap.LineNodeWest,
			Name:  "DUP",
			Ports: []mapfmt.Port{{ID: "P9", Purpose: "map_boundary"}},
		})
	}))
	rejects(t, m, "повторяет")
}

// Текст отказа называет метку, когда она есть, и UUID, когда метки нет, —
// причина при этом одна и та же. Сравнение текстов одной порчи с меткой и без
// неё: разница обязана быть только в именовании элемента.
func TestRefusalNamesTheLabel(t *testing.T) {
	spoiled := func(name string) *mapfmt.Map {
		return seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) {
			m.Topology.Edges[0].Kind = ""
			m.Topology.Edges[0].Name = name
		}))
	}
	withName := refusal(t, spoiled("E1"))
	withoutName := refusal(t, spoiled(""))
	if !strings.Contains(withName, "E1") {
		t.Fatalf("отказ с меткой не называет её: %s", withName)
	}
	if strings.Contains(withoutName, "E1") {
		t.Fatalf("отказ без метки не должен называть её: %s", withoutName)
	}
	if !strings.Contains(withoutName, seedmap.LineEdgeID) {
		t.Fatalf("отказ без метки обязан называть UUID: %s", withoutName)
	}
	for _, text := range []string{withName, withoutName} {
		if !strings.Contains(text, "не указан kind") {
			t.Fatalf("причина отказа разошлась: %s", text)
		}
	}
}
