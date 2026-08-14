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

func rejectsConstruction(t *testing.T, m *mapfmt.Map, reason string) {
	t.Helper()
	err := mapfmt.Validate(m)
	if err == nil {
		t.Fatalf("ожидался отказ (%s), карта прошла", reason)
	}
	if !strings.HasPrefix(err.Error(), "отрисовка: ") {
		t.Fatalf("отказ пришёл не от модуля отрисовки: %v", err)
	}
	if !strings.Contains(err.Error(), reason) {
		t.Fatalf("отказ %q не содержит %q", err, reason)
	}
}

// withType правит единственный тип решётки, который порождает фабрика.
func withType(f func(*mapfmt.TrackType)) seedmap.Option {
	return seedmap.Mutate(func(m *mapfmt.Map) { f(&m.Construction.Types[0]) })
}

// withRun правит run, покрывающий названный элемент. Поиск по элементу, а не
// по индексу: порядок run'ов — деталь фабрики, а тест говорит о смысле.
func withRun(element string, f func(*mapfmt.ConstructionRun)) seedmap.Option {
	return seedmap.Mutate(func(m *mapfmt.Map) {
		for i := range m.Construction.Runs {
			for _, sp := range m.Construction.Runs[i].Spans {
				if sp.Element == element {
					f(&m.Construction.Runs[i])
					return
				}
			}
		}
		panic("в карте нет run'а на элементе " + element)
	})
}

// withPlatform правит платформу станции.
func withPlatform(f func(*mapfmt.Structure)) seedmap.Option {
	return seedmap.Mutate(func(m *mapfmt.Map) {
		for i := range m.Topology.Structures {
			if m.Topology.Structures[i].Kind == "platform" {
				f(&m.Topology.Structures[i])
				return
			}
		}
		panic("в карте нет платформы")
	})
}

// span — интервал run'а: направление у решётки обязательно, она укладывается по
// ходу.
func span(element string, from, to float64) netloc.IntervalU {
	return netloc.IntervalU{Element: element, From: from, To: to, Direction: netloc.DirForward}
}

// TestConstructionRejectsTypeOutOfRange — величины типа проверяются диапазоном,
// а не знаком: шаг 0.001 м проходит «строго положительно» и даёт миллион шпал на
// километр — клиент ложится без ошибки.
func TestConstructionRejectsTypeOutOfRange(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(*mapfmt.TrackType)
		reason  string
	}{
		{"колея слишком широка", func(t *mapfmt.TrackType) { t.Gauge = 10 }, "gauge"},
		{"колея слишком узка", func(t *mapfmt.TrackType) { t.Gauge = 0.1 }, "gauge"},
		{"шаг шпал слишком мал", func(t *mapfmt.TrackType) { t.Sleeper.Pitch = 0.001 }, "sleeper.pitch"},
		{"шаг шпал слишком велик", func(t *mapfmt.TrackType) { t.Sleeper.Pitch = 3.0 }, "sleeper.pitch"},
		{"шпала слишком коротка", func(t *mapfmt.TrackType) { t.Sleeper.Length = 0.5 }, "sleeper.length"},
		{"шпала слишком узка", func(t *mapfmt.TrackType) { t.Sleeper.Width = 0.01 }, "sleeper.width"},
		{"балласт слишком узок", func(t *mapfmt.TrackType) { t.Ballast.HalfWidth = 0.1 }, "ballast.half_width"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rejectsConstruction(t, seedmap.Line(withType(c.corrupt)), c.reason)
		})
	}
}

// Два типа решётки с одним UUID — отказ. Тождество типа — UUID, поэтому повтор
// ловится сквозной проверкой уникальности (checkUniqueIDs) раньше модуля
// отрисовки: сообщение «тип решётки МЕТКА (uuid) повторяет …» называет причину
// прямо. Собственная проверка модуля («объявлен дважды») осталась страховкой на
// случай, если сквозную ослабят, и недостижима с одинаковыми UUID.
func TestConstructionRejectsDuplicateType(t *testing.T) {
	m := seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) {
		m.Construction.Types = append(m.Construction.Types, m.Construction.Types[0])
	}))
	rejects(t, m, "повторяет")
}

func TestConstructionRejectsUndeclaredDefaultType(t *testing.T) {
	m := seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) { m.Construction.DefaultType = "NOPE" }))
	rejectsConstruction(t, m, "тип по умолчанию")
}

// TestConstructionRejectsCorruptedRun — форма самого run'а: тип, координата,
// фаза, направление и пределы спанов.
func TestConstructionRejectsCorruptedRun(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(*mapfmt.ConstructionRun)
		reason  string
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
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rejectsConstruction(t, seedmap.Line(withRun(seedmap.LineEdgeID, c.corrupt)), c.reason)
		})
	}
}

// TestConstructionRequiresEdgeCoverage — ребро покрыто ровно одним run'ом, целиком,
// без пропусков и перекрытий: решётка — авторитетный факт о физическом пути, и
// дыра в покрытии означает участок, про который никто ничего не сказал.
func TestConstructionRequiresEdgeCoverage(t *testing.T) {
	const U = seedmap.LineLengthM
	cases := []struct {
		name    string
		corrupt seedmap.Option
		reason  string
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
					ID:         tID11,
					Name:       "RUN_DUP",
					Coordinate: "u",
					Spans:      netloc.LinearU{span(seedmap.LineEdgeID, 0, U)},
				})
			}),
			"ровно один run",
		},
		{
			"пропуск между спанами",
			withRun(seedmap.LineEdgeID, func(r *mapfmt.ConstructionRun) {
				r.Spans = netloc.LinearU{span(seedmap.LineEdgeID, 0, U-101), span(seedmap.LineEdgeID, U-100, U)}
			}),
			"пропуск",
		},
		{
			"перекрытие спанов",
			withRun(seedmap.LineEdgeID, func(r *mapfmt.ConstructionRun) {
				r.Spans = netloc.LinearU{span(seedmap.LineEdgeID, 0, U-80), span(seedmap.LineEdgeID, U-100, U)}
			}),
			"перекрытие",
		},
		{
			"покрытие начинается не с нуля",
			withRun(seedmap.LineEdgeID, func(r *mapfmt.ConstructionRun) { r.Spans[0].From = 1 }),
			"начинается с",
		},
		{
			"покрытие кончается раньше конца ребра",
			withRun(seedmap.LineEdgeID, func(r *mapfmt.ConstructionRun) { r.Spans[0].To = U - 0.5 }),
			"кончается на",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rejectsConstruction(t, seedmap.Line(c.corrupt), c.reason)
		})
	}
}

// TestConstructionRejectsRunOnDevicePassage — решётка устройств
// нерегулярна, run'ы её не покрывают: клиент рисует стрелку собственным
// приближением (спека §4).
func TestConstructionRejectsRunOnDevicePassage(t *testing.T) {
	m := seedmap.Station(withRun(seedmap.StationApproach, func(r *mapfmt.ConstructionRun) {
		r.Spans[0] = span(seedmap.StationSW1+mapfmt.PassageStraight, 0, 33.5)
	}))
	rejectsConstruction(t, m, "проход устройства")
}

// TestConstructionRejectsUnknownTurnoutType — крестовина считается по колее
// типа САМОГО устройства, поэтому неразрешимая ссылка — отказ, а не отложенная
// ошибка.
func TestConstructionRejectsUnknownTurnoutType(t *testing.T) {
	m := seedmap.Station(seedmap.Mutate(func(m *mapfmt.Map) {
		m.Topology.Turnouts[0].Type = "NOPE"
	}))
	rejectsConstruction(t, m, "стрелка")
}

// TestConstructionRequiresPlatformDimensions — размеры обязательны и лежат в
// правдоподобных пределах (спека §3). Пропущенное поле — ноль, а ноль вне
// диапазона: обязательность обеспечена той же формулой !(v >= min && v <= max),
// что и у типа, — валидатор не полагается на проверку конечности.
func TestConstructionRequiresPlatformDimensions(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(*mapfmt.Structure)
		reason  string
	}{
		{"offset пропущен", func(st *mapfmt.Structure) { st.Offset = 0 }, "offset"},
		{"offset слишком мал", func(st *mapfmt.Structure) { st.Offset = 0.5 }, "offset"},
		{"offset слишком велик", func(st *mapfmt.Structure) { st.Offset = 10 }, "offset"},
		{"width пропущена", func(st *mapfmt.Structure) { st.Width = 0 }, "width"},
		{"width слишком мала", func(st *mapfmt.Structure) { st.Width = 0.3 }, "width"},
		{"width слишком велика", func(st *mapfmt.Structure) { st.Width = 30 }, "width"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rejectsConstruction(t, seedmap.Station(withPlatform(c.corrupt)), c.reason)
		})
	}
}

// TestPlatformDimensionsAreCheckedWithoutConstructionBlock — размеры платформы часть
// контракта отрисовки, а не блока рецепта: карта с голой платформой не должна
func TestPlatformDimensionsAreCheckedWithoutConstructionBlock(t *testing.T) {
	bare := mapfmt.Structure{
		ID:   tID07,
		Name: "PLAT",
		Kind: "platform",
		Span: netloc.LinearU{{Element: seedmap.LineEdgeID, From: 0, To: 50}},
		Side: "right",
	}
	rejectsConstruction(t,
		seedmap.Line(seedmap.WithoutConstruction(), seedmap.WithStructure(bare)), "offset")

	withDimensions := bare
	withDimensions.Offset = 1.745
	withDimensions.Width = 3
	// Вертикаль с редакции 6 так же обязательна, как ширина, и по тому же
	// доводу: платформа без высоты не рисуется, а умолчание в клиенте
	// запрещено. Проверяем это отдельным отказом, чтобы «забыли height» не
	// пряталось за «забыли offset».
	rejectsConstruction(t,
		seedmap.Line(seedmap.WithoutConstruction(), seedmap.WithStructure(withDimensions)), "height")
	withDimensions.Height = 0.2
	withDimensions.SlabThickness = 0.35
	accepts(t, seedmap.Line(seedmap.WithoutConstruction(), seedmap.WithStructure(withDimensions)))
}

// TestBufferStopNeedsDimensionsAndDeclaredDeadEnd — упор с 2026-08-12 несёт
// габарит и обязан стоять там, где тупик ОБЪЯВЛЕН топологией.
//
// # Что здесь изменилось и почему прежнее утверждение умерло
//
// Тест звался TestBufferStopCarriesNoDimensions и утверждал обратное: упор —
// точечный объект, диапазоны к нему не применяются. Утверждение держалось на
// том, что упор никто не рисует: затравка его не создавала (дыра Д8 разбора
// спайка), а снесённый клиент выводил упоры ИЗ ТОПОЛОГИИ сам, то есть рисовал
// то, чего ему не присылали. Оба основания отпали разом.
//
// Проверка порта — лечение двойного выражения одного факта (редакция 6 §4.3):
// Port.Purpose объявляет тупик, Structure его подтверждает, и подтверждение
// неутверждённого есть расхождение двух записей, а расхождение обязано быть
// отказом.
func TestBufferStopNeedsDimensionsAndDeclaredDeadEnd(t *testing.T) {
	at := netloc.LinearU{{Element: seedmap.LineEdgeID, From: seedmap.LineLengthM, To: seedmap.LineLengthM}}
	bare := mapfmt.Structure{ID: tID13, Name: "BS", Kind: "buffer_stop", Span: at}

	// Перегон кончается границей карты, а не тупиком: сооружение утверждает то,
	// чего топология не объявляла.
	rejects(t, seedmap.Line(seedmap.WithStructure(bare)), "топология его не объявляет")

	// На станции конец главного пути объявлен тупиковым портом — там упор
	// законен, и без габарита он отвергается уже по размерам.
	noSize := mapfmt.Structure{
		ID:   tID14,
		Name: "BS_X",
		Kind: "buffer_stop",
		Span: netloc.LinearU{{Element: seedmap.StationMain, From: 230, To: 230}},
	}
	rejectsConstruction(t, seedmap.Station(seedmap.WithStructure(noSize)), "height")
}
