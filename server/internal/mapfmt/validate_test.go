package mapfmt_test

import (
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// TestВалидаторОтвергает — правила формы, каждое доказано своей порчей карты
// фабрики. Карта до порчи валидна (это доказано тестом фабрики), поэтому отказ
// приходит ровно от внесённого дефекта.
func TestВалидаторОтвергает(t *testing.T) {
	случаи := []struct {
		имя     string
		карта   *mapfmt.Map
		причина string
	}{
		{
			"домены выравниваний не совпадают",
			seedmap.Line(геометрияПерегона(
				[]mapfmt.HPrim{прямая(seedmap.LineLengthM)},
				[]mapfmt.VPrim{{Kind: "grade", Length: seedmap.LineLengthM - 10}})),
			"домен",
		},
		{
			"вертикаль начинается не с grade",
			seedmap.Line(геометрияПерегона(
				[]mapfmt.HPrim{прямая(seedmap.LineLengthM)},
				[]mapfmt.VPrim{{Kind: "vertical_curve", Length: seedmap.LineLengthM, EndSlopePermille: 5}})),
			"grade",
		},
		{
			"нулевая длина примитива",
			seedmap.Line(геометрияПерегона([]mapfmt.HPrim{прямая(0)}, nil)),
			"длина",
		},
		{
			"неизвестный примитив плана",
			seedmap.Line(геометрияПерегона(
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
			"путевой объект неизвестного рода",
			seedmap.Line(seedmap.WithTrackside(mapfmt.Trackside{
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
	for _, c := range случаи {
		t.Run(c.имя, func(t *testing.T) { отвергает(t, c.карта, c.причина) })
	}
}

// TestВидЭлементаОбязателен — карта без kind получает ОТКАЗ НА ВХОДЕ, а не
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
func TestВидЭлементаОбязателен(t *testing.T) {
	случаи := []struct {
		имя     string
		карта   *mapfmt.Map
		причина string
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
	for _, c := range случаи {
		t.Run(c.имя, func(t *testing.T) { отвергает(t, c.карта, c.причина) })
	}
}

// TestПортСтрелкиБезРебраОтвергается — отрезаем тупик целиком: порт SW2.D
// остаётся вообще без ребра. Это отказ про не соединённый порт устройства, а не
// про висящий конец с просьбой назначить purpose: у стрелки все три порта
// обязаны нести ребро.
func TestПортСтрелкиБезРебраОтвергается(t *testing.T) {
	m := seedmap.Station(безРебра(seedmap.StationStub))
	отвергает(t, m, "не соединён")
}

// TestМаркаКрестовиныНеобязательна — §8 объявляет марку происхождением
// геометрии, а не ограничением: карта без неё законна, клиент строит крестовину
// из геометрии.
func TestМаркаКрестовиныНеобязательна(t *testing.T) {
	m := seedmap.Station(seedmap.Mutate(func(m *mapfmt.Map) {
		for i := range m.Topology.Turnouts {
			m.Topology.Turnouts[i].Frog = ""
		}
	}))
	принимает(t, m)
}

// безРебра убирает ребро целиком: топологию, геометрию и покрывающий его run.
// Убирать всё сразу обязательно — иначе карта ломается в трёх местах и отказ
// приходит по первому попавшемуся, а не по предмету теста.
func безРебра(ид string) seedmap.Option {
	return seedmap.Mutate(func(m *mapfmt.Map) {
		рёбра := m.Topology.Edges[:0]
		for _, e := range m.Topology.Edges {
			if e.ID != ид {
				рёбра = append(рёбра, e)
			}
		}
		m.Topology.Edges = рёбра
		delete(m.Geometry.Edges, ид)
		if m.Construction == nil {
			return
		}
		прогоны := m.Construction.Runs[:0]
		for _, r := range m.Construction.Runs {
			покрывает := false
			for _, sp := range r.Spans {
				if sp.Element == ид {
					покрывает = true
				}
			}
			if !покрывает {
				прогоны = append(прогоны, r)
			}
		}
		m.Construction.Runs = прогоны
	})
}
