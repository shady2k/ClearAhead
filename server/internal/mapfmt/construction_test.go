package mapfmt_test

import (
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// Модуль «отрисовка»: типы путевых конструкций, run'ы размещения решётки,
// покрытие рёбер и размеры платформ (спека контракта отрисовки §3–4).
//
// Тесты зовут Validate целиком, а не модуль: изнутри внешнего пакета
// неэкспортированный validateConstruction не виден, и это к лучшему — проверка
// префикса «отрисовка: » заодно доказывает, что отказ пришёл именно от этого
// модуля, а не от структурных проверок, которые зовутся раньше.

func отвергаетОтрисовка(t *testing.T, m *mapfmt.Map, причина string) {
	t.Helper()
	err := mapfmt.Validate(m)
	if err == nil {
		t.Fatalf("ожидался отказ (%s), карта прошла", причина)
	}
	if !strings.HasPrefix(err.Error(), "отрисовка: ") {
		t.Fatalf("отказ пришёл не от модуля отрисовки: %v", err)
	}
	if !strings.Contains(err.Error(), причина) {
		t.Fatalf("отказ %q не содержит %q", err, причина)
	}
}

// сТипом правит единственный тип решётки, который порождает фабрика.
func сТипом(f func(*mapfmt.TrackType)) seedmap.Option {
	return seedmap.Mutate(func(m *mapfmt.Map) { f(&m.Construction.Types[0]) })
}

// сПрогоном правит run, покрывающий названный элемент. Поиск по элементу, а не
// по индексу: порядок run'ов — деталь фабрики, а тест говорит о смысле.
func сПрогоном(элемент string, f func(*mapfmt.ConstructionRun)) seedmap.Option {
	return seedmap.Mutate(func(m *mapfmt.Map) {
		for i := range m.Construction.Runs {
			for _, sp := range m.Construction.Runs[i].Spans {
				if sp.Element == элемент {
					f(&m.Construction.Runs[i])
					return
				}
			}
		}
		panic("в карте нет run'а на элементе " + элемент)
	})
}

// сПлатформой правит платформу станции.
func сПлатформой(f func(*mapfmt.Trackside)) seedmap.Option {
	return seedmap.Mutate(func(m *mapfmt.Map) {
		for i := range m.Topology.Trackside {
			if m.Topology.Trackside[i].Kind == "platform" {
				f(&m.Topology.Trackside[i])
				return
			}
		}
		panic("в карте нет платформы")
	})
}

// спан — интервал run'а: направление у решётки обязательно, она укладывается по
// ходу.
func спан(элемент string, от, до float64) netloc.IntervalU {
	return netloc.IntervalU{Element: элемент, From: от, To: до, Direction: netloc.DirForward}
}

// TestОтрисовкаОтвергаетТипВнеДиапазона — величины типа проверяются диапазоном,
// а не знаком: шаг 0.001 м проходит «строго положительно» и даёт миллион шпал на
// километр — клиент ложится без ошибки.
func TestОтрисовкаОтвергаетТипВнеДиапазона(t *testing.T) {
	случаи := []struct {
		имя     string
		порча   func(*mapfmt.TrackType)
		причина string
	}{
		{"колея слишком широка", func(t *mapfmt.TrackType) { t.Gauge = 10 }, "gauge"},
		{"колея слишком узка", func(t *mapfmt.TrackType) { t.Gauge = 0.1 }, "gauge"},
		{"шаг шпал слишком мал", func(t *mapfmt.TrackType) { t.Sleeper.Pitch = 0.001 }, "sleeper.pitch"},
		{"шаг шпал слишком велик", func(t *mapfmt.TrackType) { t.Sleeper.Pitch = 3.0 }, "sleeper.pitch"},
		{"шпала слишком коротка", func(t *mapfmt.TrackType) { t.Sleeper.Length = 0.5 }, "sleeper.length"},
		{"шпала слишком узка", func(t *mapfmt.TrackType) { t.Sleeper.Width = 0.01 }, "sleeper.width"},
		{"балласт слишком узок", func(t *mapfmt.TrackType) { t.Ballast.HalfWidth = 0.1 }, "ballast.half_width"},
	}
	for _, c := range случаи {
		t.Run(c.имя, func(t *testing.T) {
			отвергаетОтрисовка(t, seedmap.Line(сТипом(c.порча)), c.причина)
		})
	}
}

func TestОтрисовкаОтвергаетПовторТипа(t *testing.T) {
	m := seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) {
		m.Construction.Types = append(m.Construction.Types, m.Construction.Types[0])
	}))
	отвергаетОтрисовка(t, m, "объявлен дважды")
}

func TestОтрисовкаОтвергаетНеобъявленныйТипПоУмолчанию(t *testing.T) {
	m := seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) { m.Construction.DefaultType = "NOPE" }))
	отвергаетОтрисовка(t, m, "тип по умолчанию")
}

// TestОтрисовкаОтвергаетПорчуПрогона — форма самого run'а: тип, координата,
// фаза, направление и пределы спанов.
func TestОтрисовкаОтвергаетПорчуПрогона(t *testing.T) {
	случаи := []struct {
		имя     string
		порча   func(*mapfmt.ConstructionRun)
		причина string
	}{
		{"неизвестный тип решётки", func(r *mapfmt.ConstructionRun) { r.Type = "NOPE" }, "неизвестный тип"},
		{"координата не u", func(r *mapfmt.ConstructionRun) { r.Coordinate = "s" }, "coordinate"},
		{"фаза равна шагу", func(r *mapfmt.ConstructionRun) { r.Phase = 0.6 }, "phase"},
		{"фаза больше шага", func(r *mapfmt.ConstructionRun) { r.Phase = 0.7 }, "phase"},
		{"фаза отрицательна", func(r *mapfmt.ConstructionRun) { r.Phase = -0.1 }, "phase"},
		{"пустая протяжённость", func(r *mapfmt.ConstructionRun) { r.Spans = nil }, "пустая протяжённость"},
		{
			"недопустимое направление спана",
			func(r *mapfmt.ConstructionRun) { r.Spans[0].Direction = "left" },
			"направление",
		},
		{
			"спан без направления",
			func(r *mapfmt.ConstructionRun) { r.Spans[0].Direction = netloc.DirNone },
			"обязано быть направление",
		},
		{
			"спан на несуществующем элементе",
			func(r *mapfmt.ConstructionRun) { r.Spans[0].Element = "E9" },
			"не существует",
		},
		{
			"вырожденный спан",
			func(r *mapfmt.ConstructionRun) { r.Spans[0].From, r.Spans[0].To = 40, 40 },
			"вырожден",
		},
		{
			"спан за пределами элемента",
			func(r *mapfmt.ConstructionRun) { r.Spans[0].To = seedmap.LineLengthM + 0.001 },
			"за пределами",
		},
	}
	for _, c := range случаи {
		t.Run(c.имя, func(t *testing.T) {
			отвергаетОтрисовка(t, seedmap.Line(сПрогоном(seedmap.LineEdgeID, c.порча)), c.причина)
		})
	}
}

// TestОтрисовкаТребуетПокрытияРёбер — ребро покрыто ровно одним run'ом, целиком,
// без пропусков и перекрытий: решётка — авторитетный факт о физическом пути, и
// дыра в покрытии означает участок, про который никто ничего не сказал.
func TestОтрисовкаТребуетПокрытияРёбер(t *testing.T) {
	const U = seedmap.LineLengthM
	случаи := []struct {
		имя     string
		порча   seedmap.Option
		причина string
	}{
		{
			"ребро не покрыто ни одним run",
			seedmap.Mutate(func(m *mapfmt.Map) { m.Construction.Runs = nil }),
			"не покрыто ни одним run",
		},
		{
			"ребро покрыто двумя run'ами",
			seedmap.Mutate(func(m *mapfmt.Map) {
				m.Construction.Runs = append(m.Construction.Runs, mapfmt.ConstructionRun{
					ID:         "RUN_DUP",
					Coordinate: "u",
					Spans:      netloc.LinearU{спан(seedmap.LineEdgeID, 0, U)},
				})
			}),
			"ровно один run",
		},
		{
			"пропуск между спанами",
			сПрогоном(seedmap.LineEdgeID, func(r *mapfmt.ConstructionRun) {
				r.Spans = netloc.LinearU{спан(seedmap.LineEdgeID, 0, U-101), спан(seedmap.LineEdgeID, U-100, U)}
			}),
			"пропуск",
		},
		{
			"перекрытие спанов",
			сПрогоном(seedmap.LineEdgeID, func(r *mapfmt.ConstructionRun) {
				r.Spans = netloc.LinearU{спан(seedmap.LineEdgeID, 0, U-80), спан(seedmap.LineEdgeID, U-100, U)}
			}),
			"перекрытие",
		},
		{
			"покрытие начинается не с нуля",
			сПрогоном(seedmap.LineEdgeID, func(r *mapfmt.ConstructionRun) { r.Spans[0].From = 1 }),
			"начинается с",
		},
		{
			"покрытие кончается раньше конца ребра",
			сПрогоном(seedmap.LineEdgeID, func(r *mapfmt.ConstructionRun) { r.Spans[0].To = U - 0.5 }),
			"кончается на",
		},
	}
	for _, c := range случаи {
		t.Run(c.имя, func(t *testing.T) {
			отвергаетОтрисовка(t, seedmap.Line(c.порча), c.причина)
		})
	}
}

// TestОтрисовкаОтвергаетПрогонНаПроходеУстройства — решётка устройств
// нерегулярна, run'ы её не покрывают: клиент рисует стрелку собственным
// приближением (спека §4).
func TestОтрисовкаОтвергаетПрогонНаПроходеУстройства(t *testing.T) {
	m := seedmap.Station(сПрогоном(seedmap.StationApproach, func(r *mapfmt.ConstructionRun) {
		r.Spans[0] = спан(seedmap.StationSW1+mapfmt.PassageStraight, 0, 33.5)
	}))
	отвергаетОтрисовка(t, m, "проход устройства")
}

// TestОтрисовкаОтвергаетНеизвестныйТипСтрелки — крестовина считается по колее
// типа САМОГО устройства, поэтому неразрешимая ссылка — отказ, а не отложенная
// ошибка.
func TestОтрисовкаОтвергаетНеизвестныйТипСтрелки(t *testing.T) {
	m := seedmap.Station(seedmap.Mutate(func(m *mapfmt.Map) {
		m.Topology.Turnouts[0].Type = "NOPE"
	}))
	отвергаетОтрисовка(t, m, "стрелка")
}

// TestОтрисовкаТребуетРазмеровПлатформы — размеры обязательны и лежат в
// правдоподобных пределах (спека §3). Пропущенное поле — ноль, а ноль вне
// диапазона: обязательность обеспечена той же формулой !(v >= min && v <= max),
// что и у типа, — валидатор не полагается на проверку конечности.
func TestОтрисовкаТребуетРазмеровПлатформы(t *testing.T) {
	случаи := []struct {
		имя     string
		порча   func(*mapfmt.Trackside)
		причина string
	}{
		{"offset пропущен", func(ts *mapfmt.Trackside) { ts.Offset = 0 }, "offset"},
		{"offset слишком мал", func(ts *mapfmt.Trackside) { ts.Offset = 0.5 }, "offset"},
		{"offset слишком велик", func(ts *mapfmt.Trackside) { ts.Offset = 10 }, "offset"},
		{"width пропущена", func(ts *mapfmt.Trackside) { ts.Width = 0 }, "width"},
		{"width слишком мала", func(ts *mapfmt.Trackside) { ts.Width = 0.3 }, "width"},
		{"width слишком велика", func(ts *mapfmt.Trackside) { ts.Width = 30 }, "width"},
	}
	for _, c := range случаи {
		t.Run(c.имя, func(t *testing.T) {
			отвергаетОтрисовка(t, seedmap.Station(сПлатформой(c.порча)), c.причина)
		})
	}
}

// TestРазмерыПлатформыПроверяютсяБезБлокаРешётки — размеры платформы часть
// контракта отрисовки, а не блока рецепта: карта с голой платформой не должна
// выйти наружу, даже если решётка ещё не авторилась.
func TestРазмерыПлатформыПроверяютсяБезБлокаРешётки(t *testing.T) {
	голая := mapfmt.Trackside{
		ID:   "PLAT",
		Kind: "platform",
		Span: netloc.LinearU{{Element: seedmap.LineEdgeID, From: 0, To: 50}},
		Side: "right",
	}
	отвергаетОтрисовка(t,
		seedmap.Line(seedmap.WithoutConstruction(), seedmap.WithTrackside(голая)), "offset")

	сРазмерами := голая
	сРазмерами.Offset = 1.75
	сРазмерами.Width = 3
	принимает(t, seedmap.Line(seedmap.WithoutConstruction(), seedmap.WithTrackside(сРазмерами)))
}

// TestУпорРазмеровНеНесёт — buffer_stop точечный объект: диапазоны платформы к
// нему не применяются.
func TestУпорРазмеровНеНесёт(t *testing.T) {
	m := seedmap.Line(seedmap.WithTrackside(mapfmt.Trackside{
		ID:   "BS",
		Kind: "buffer_stop",
		Span: netloc.LinearU{{Element: seedmap.LineEdgeID, From: seedmap.LineLengthM, To: seedmap.LineLengthM}},
	}))
	принимает(t, m)
}
