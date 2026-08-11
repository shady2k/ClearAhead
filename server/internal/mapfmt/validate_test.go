package mapfmt_test

import (
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// TestValidatorRejects — правила формы, каждое доказано своей порчей карты
// фабрики. Карта до порчи валидна (это доказано тестом фабрики), поэтому отказ
// приходит ровно от внесённого дефекта.
func TestValidatorRejects(t *testing.T) {
	cases := []struct {
		name   string
		m      *mapfmt.Map
		reason string
	}{
		{
			"домены выравниваний не совпадают",
			seedmap.Line(lineGeometry(
				[]mapfmt.HPrim{straight(seedmap.LineLengthM)},
				[]mapfmt.VPrim{{Kind: "grade", Length: seedmap.LineLengthM - 10}})),
			"домен",
		},
		{
			"вертикаль начинается не с grade",
			seedmap.Line(lineGeometry(
				[]mapfmt.HPrim{straight(seedmap.LineLengthM)},
				[]mapfmt.VPrim{{Kind: "vertical_curve", Length: seedmap.LineLengthM, EndSlopePermille: 5}})),
			"grade",
		},
		{
			"нулевая длина примитива",
			seedmap.Line(lineGeometry([]mapfmt.HPrim{straight(0)}, nil)),
			"длина",
		},
		{
			"неизвестный примитив плана",
			seedmap.Line(lineGeometry(
				[]mapfmt.HPrim{{Kind: "spiral", Length: seedmap.LineLengthM}}, nil)),
			"неизвестный",
		},
		{
			"геопривязка с недопустимым origin_height_kind",
			seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) {
				m.Georeference = &mapfmt.Georeference{Datum: "WGS84", OriginHeightKind: "geoid"}
			})),
			"origin_height_kind",
		},
		{
			"сооружение неизвестного вида",
			seedmap.Line(seedmap.WithStructure(mapfmt.Structure{
				ID:   "TS1",
				Kind: "sarai",
				Span: netloc.LinearU{{Element: seedmap.LineEdgeID, From: 0, To: 10}},
			})),
			"неизвестный kind",
		},
		{
			"ребро начинается и кончается в одном порту",
			seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) {
				m.Topology.Edges[0].To = m.Topology.Edges[0].From
			})),
			"в одном порту",
		},
		{
			"якорь на несуществующем порту",
			seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) {
				m.Anchors = map[string]mapfmt.Anchor{"N9.P1": {}}
			})),
			"якорь ссылается на несуществующий порт",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { rejects(t, c.m, c.reason) })
	}
}

// TestElementKindIsRequired — карта без kind получает ОТКАЗ НА ВХОДЕ, а не
// молчаливое умолчание «значит рельсы».
//
// Это приёмочный критерий ClearAhead-z4u и главное, ради чего поле заведено
// раньше второго вида: умолчание проставило бы за автора утверждение, которого
// он не делал, и день появления дорог начался бы с разбора, что означает
// отсутствие поля в каждой уже существующей карте.
//
// Проверяются оба носителя вида — ребро (его пишет автор) и стрелка (от неё
// вид достаётся её проходам), — и обе половины отказа: пустое значение и
// незнакомое. Пустое отвергается своим текстом: это не опечатка, а карта,
// написанная до правила, и сказать автору надо именно это.
func TestElementKindIsRequired(t *testing.T) {
	cases := []struct {
		name   string
		m      *mapfmt.Map
		reason string
	}{
		{
			"у ребра нет вида",
			seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) { m.Topology.Edges[0].Kind = "" })),
			"не указан kind",
		},
		{
			"у ребра незнакомый вид",
			seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) { m.Topology.Edges[0].Kind = "road" })),
			`неизвестный вид "road"`,
		},
		{
			"у стрелки нет вида",
			seedmap.Station(seedmap.Mutate(func(m *mapfmt.Map) { m.Topology.Turnouts[0].Kind = "" })),
			"не указан kind",
		},
		{
			"у стрелки незнакомый вид",
			seedmap.Station(seedmap.Mutate(func(m *mapfmt.Map) { m.Topology.Turnouts[0].Kind = "monorail" })),
			`неизвестный вид "monorail"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { rejects(t, c.m, c.reason) })
	}
}

// TestTurnoutPortWithoutEdgeIsRejected — отрезаем тупик целиком: порт SW2.D
// остаётся вообще без ребра. Это отказ про не соединённый порт устройства, а не
// про висящий конец с просьбой назначить purpose: у стрелки все три порта
// обязаны нести ребро.
func TestTurnoutPortWithoutEdgeIsRejected(t *testing.T) {
	m := seedmap.Station(withoutEdge(seedmap.StationStub))
	rejects(t, m, "не соединён")
}

// TestFrogNumberIsOptional — §8 объявляет марку происхождением
// геометрии, а не ограничением: карта без неё законна, клиент строит крестовину
// из геометрии.
func TestFrogNumberIsOptional(t *testing.T) {
	m := seedmap.Station(seedmap.Mutate(func(m *mapfmt.Map) {
		for i := range m.Topology.Turnouts {
			m.Topology.Turnouts[i].Frog = ""
		}
	}))
	accepts(t, m)
}

// withoutEdge убирает ребро целиком: топологию, геометрию и покрывающий его run.
// Убирать всё сразу обязательно — иначе карта ломается в трёх местах и отказ
// приходит по первому попавшемуся, а не по предмету теста.
func withoutEdge(id string) seedmap.Option {
	return seedmap.Mutate(func(m *mapfmt.Map) {
		edges := m.Topology.Edges[:0]
		for _, e := range m.Topology.Edges {
			if e.ID != id {
				edges = append(edges, e)
			}
		}
		m.Topology.Edges = edges
		delete(m.Geometry.Edges, id)
		if m.Construction == nil {
			return
		}
		runs := m.Construction.Runs[:0]
		for _, r := range m.Construction.Runs {
			covers := false
			for _, sp := range r.Spans {
				if sp.Element == id {
					covers = true
				}
			}
			if !covers {
				runs = append(runs, r)
			}
		}
		m.Construction.Runs = runs
	})
}
