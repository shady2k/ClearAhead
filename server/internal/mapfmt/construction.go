package mapfmt

import (
	"fmt"
	"math"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/units"
)

// validateConstruction — модуль «отрисовка»: типы путевых конструкций, run'ы
// размещения, покрытие рёбер и размеры платформ (спека контракта отрисовки
// §3–4, план волны 2a, задачи 1–2).
//
// Блок construction отсутствует — решётки нет, рецепта нет: модуль пропускает
// рецепт. Это сознательное решение, а не умолчание по недосмотру: блок
// порождает строитель (авторинг), и карта, где решётка ещё не авторилась,
// законна — в проводе ей соответствуют пустые track_types и construction_runs.
// Присутствует блок — проверяется полностью: форма, ссылки, диапазоны,
// покрытие. Размеры платформ (§3) проверяются в любом случае — они часть
// контракта отрисовки, а не блока рецепта (см. checkPlatformSizes).
//
// Каждый отказ начинается с префикса «отрисовка: » — модуль называет себя в
// тексте отказа (см. Validate).
func (m *Map) validateConstruction() error {
	// КАТАЛОГ ТИПОВ УСТРОЙСТВ проверяется первым: ссылки стрелок на него не
	// зависят от блока construction (карта без него тоже обязана получить отказ,
	// если в ней есть стрелка), и порядок здесь называет эту независимость.
	const prefix = "отрисовка: "
	// Каталог называет себя тем же модулем, что и решётка: он часть того же
	// рецепта конструкции, и отказ, приходящий без имени модуля, читался бы как
	// пришедший ниоткуда.
	if err := validateTurnoutTypes(m); err != nil {
		return fmt.Errorf("%s%w", prefix, err)
	}
	// Размеры платформ проверяются до раннего выхода: карта с платформой без
	// размеров не должна выйти наружу, даже если решётка ещё не авторилась.
	if err := m.checkPlatformSizes(prefix); err != nil {
		return err
	}
	c := m.Construction
	if c == nil {
		return nil
	}

	// Типы: лимит длины, уникальные id, величины в правдоподобных диапазонах.
	// Диапазоны, а не знак: шаг 0.001 м проходит «строго положительно» и даёт
	// миллион шпал на километр — клиент ложится без ошибки.
	types := make(map[string]TrackType, len(c.Types))
	if len(c.Types) > MaxTrackTypes {
		return fmt.Errorf("%sтипов больше %d", prefix, MaxTrackTypes)
	}
	for i := range c.Types {
		t := &c.Types[i]
		if err := checkTrackType(prefix, t); err != nil {
			return err
		}
		if _, dup := types[t.ID]; dup {
			return fmt.Errorf("%sтип %q объявлен дважды", prefix, t.ID)
		}
		types[t.ID] = *t
	}
	// БЛОК С ОДНИМ ЛИШЬ КАТАЛОГОМ ЗАКОНЕН, и это не поблажка. default_type
	// отвечает на вопрос «какой тип ПУТИ подставить run'у, у которого он
	// опущен»; там, где нет ни типов, ни run'ов, вопроса не существует. Карта,
	// объявившая проекты переводов и ещё не авторившая решётку, — законное
	// промежуточное состояние, и требовать от неё умолчания значило бы требовать
	// ответа на незаданный вопрос.
	if c.DefaultType == "" && len(c.Types) == 0 && len(c.Runs) == 0 {
		return nil
	}
	if c.DefaultType == "" {
		return fmt.Errorf("%sdefault_type не задан", prefix)
	}
	if _, ok := types[c.DefaultType]; !ok {
		return fmt.Errorf("%sтип по умолчанию %q не объявлен в types", prefix, c.DefaultType)
	}

	// Тип устройства: опущен — применяется умолчание, явный — обязан
	// существовать. Крестовина (§5) считается по колее типа САМОГО устройства,
	// поэтому неразрешимая ссылка здесь — отказ, а не отложенная ошибка.
	for i := range m.Topology.Turnouts {
		t := &m.Topology.Turnouts[i]
		if t.Type != "" {
			if _, ok := types[t.Type]; !ok {
				return fmt.Errorf("%sстрелка %q: неизвестный тип %q", prefix, t.ID, t.Type)
			}
		}
		// БРУСЬЯ ОБЯЗАТЕЛЬНЫ У ТИПА УСТРОЙСТВА, и отказ здесь — исполнение той же
		// границы, что и у крестовины: run'ами проходы стрелки не покрываются
		// (§4), значит решётку под ней взять больше неоткуда. Молчаливое
		// умолчание дало бы стрелку без единого бруса — ровно то, чем была
		// найдена ClearAhead-7kv: 22.5 % пути без решётки при зелёной карте.
		//
		// Проверяется РАЗРЕШЁННЫЙ тип, а не записанный: стрелка вправе тип
		// опустить, и тогда обязанность ложится на default_type.
		resolved := t.Type
		if resolved == "" {
			resolved = c.DefaultType
		}
		if tt, ok := types[resolved]; ok && tt.Timber == nil {
			return fmt.Errorf(
				"%sстрелка %s: у типа %s нет блока timber — под переводом лежат не шпалы, и эпюру брусьев взять неоткуда",
				prefix, Labeled(t.Name, t.ID), Labeled(tt.Name, resolved))
		}
	}

	domains, err := m.elementDomains()
	if err != nil {
		return fmt.Errorf("%s%s", prefix, err)
	}
	passages := m.passageIDs()

	// Покрытие рёбер. Каждое ребро покрыто ровно одним run, без пропусков и
	// перекрытий; проходы устройств run'ами НЕ покрываются — их решётка
	// нерегулярна, это уровень 3, и устройство рисуется собственным
	// приближением клиента (спека §4).
	runFor := make(map[string]string, len(c.Runs)) // ребро → id run'а
	spansFor := make(map[string][]runSpanU, len(c.Runs))
	if len(c.Runs) > MaxConstructionRuns {
		return fmt.Errorf("%srun'ов больше %d", prefix, MaxConstructionRuns)
	}
	seenRuns := make(map[string]bool, len(c.Runs))
	for i := range c.Runs {
		r := &c.Runs[i]
		if err := checkEntity("run решётки", r.Name, r.ID); err != nil {
			return fmt.Errorf("%s%w", prefix, err)
		}
		if seenRuns[r.ID] {
			return fmt.Errorf("%srun %q объявлен дважды", prefix, Labeled(r.Name, r.ID))
		}
		seenRuns[r.ID] = true
		typ := r.Type
		if typ == "" {
			typ = c.DefaultType
		}
		if _, ok := types[typ]; !ok {
			return fmt.Errorf("%srun %q: неизвестный тип %q", prefix, Labeled(r.Name, r.ID), typ)
		}
		if r.Coordinate != "u" {
			return fmt.Errorf("%srun %q: coordinate %q, разрешено только \"u\"", prefix, Labeled(r.Name, r.ID), r.Coordinate)
		}
		// Фаза нормализована относительно шага РАЗРЕШЁННОГО типа run'а:
		// полуоткрытое правило размещения phase + n·pitch ∈ [0, run_length).
		pitch := types[typ].Sleeper.Pitch
		if !(r.Phase >= 0 && r.Phase < pitch) {
			return fmt.Errorf("%srun %q: phase %g вне [0, pitch=%g)", prefix, Labeled(r.Name, r.ID), r.Phase, pitch)
		}
		if err := r.Spans.Structural(); err != nil {
			return fmt.Errorf("%srun %q: %w", prefix, Labeled(r.Name, r.ID), err)
		}
		if len(r.Spans) > MaxRunSpans {
			return fmt.Errorf("%srun %q: спанов больше %d", prefix, Labeled(r.Name, r.ID), MaxRunSpans)
		}
		// У run'а решётки направление ОБЯЗАТЕЛЬНО у каждого спана: решётка
		// укладывается по ходу, и спан без направления в ней недоописан — в
		// отличие от платформы, у которой направления нет по существу.
		if !r.Spans.Directed() {
			return fmt.Errorf("%srun %q: у каждого спана обязано быть направление forward или reverse",
				prefix, Labeled(r.Name, r.ID))
		}
		for j := range r.Spans {
			sp := &r.Spans[j]
			dom, ok := domains[sp.Element]
			if !ok {
				return fmt.Errorf("%srun %q: спана %d: элемент %q не существует", prefix, Labeled(r.Name, r.ID), j, sp.Element)
			}
			if passages[sp.Element] {
				return fmt.Errorf("%srun %q: спана %d покрывает проход устройства %q — решётка устройств нерегулярна, run'ы её не покрывают",
					prefix, Labeled(r.Name, r.ID), j, sp.Element)
			}
			from, err := units.MetersToDistance(sp.From)
			if err != nil {
				return fmt.Errorf("%srun %q: спана %d: %w", prefix, Labeled(r.Name, r.ID), j, err)
			}
			to, err := units.MetersToDistance(sp.To)
			if err != nil {
				return fmt.Errorf("%srun %q: спана %d: %w", prefix, Labeled(r.Name, r.ID), j, err)
			}
			if !(from >= 0 && to > from) {
				return fmt.Errorf("%srun %q: спана %d: интервал [%g, %g] вырожден",
					prefix, Labeled(r.Name, r.ID), j, sp.From, sp.To)
			}
			if to > dom {
				return fmt.Errorf("%srun %q: спана %d: конец %g за пределами элемента %q (длина %s)",
					prefix, Labeled(r.Name, r.ID), j, sp.To, sp.Element, dom)
			}
			if other, dup := runFor[sp.Element]; dup && other != r.ID {
				return fmt.Errorf("%sребро %q покрыто и run'ом %q, и run'ом %q — ровно один run на ребро",
					prefix, sp.Element, other, r.ID)
			}
			runFor[sp.Element] = r.ID
			spansFor[sp.Element] = append(spansFor[sp.Element], runSpanU{from: from, to: to})
		}
	}

	// Каждое ребро покрыто, и спаны покрывающего run'а смыкаются в [0, U] без
	// пропусков и перекрытий. Допуск на стыках — один микрометр: шум округления
	// автора, невидимый ни одному клиенту.
	const abutTolerance = units.Micrometer
	for _, e := range m.Topology.Edges {
		spans, ok := spansFor[e.ID]
		if !ok {
			return fmt.Errorf("%sребро %q не покрыто ни одним run", prefix, Labeled(e.Name, e.ID))
		}
		sort.Slice(spans, func(i, j int) bool { return spans[i].from < spans[j].from })
		if spans[0].from > abutTolerance {
			return fmt.Errorf("%sребро %q: покрытие начинается с %s, ожидается 0", prefix, Labeled(e.Name, e.ID), spans[0].from)
		}
		u := domains[e.ID]
		if u-spans[len(spans)-1].to > abutTolerance {
			return fmt.Errorf("%sребро %q: покрытие кончается на %s, ожидается %s",
				prefix, Labeled(e.Name, e.ID), spans[len(spans)-1].to, u)
		}
		for i := 1; i < len(spans); i++ {
			gap := spans[i].from - spans[i-1].to
			if gap < -abutTolerance {
				return fmt.Errorf("%sребро %q: перекрытие спанов %s–%s и %s–%s",
					prefix, Labeled(e.Name, e.ID), spans[i-1].from, spans[i-1].to, spans[i].from, spans[i].to)
			}
			if gap > abutTolerance {
				return fmt.Errorf("%sребро %q: пропуск между спанами %s и %s",
					prefix, Labeled(e.Name, e.ID), spans[i-1].to, spans[i].from)
			}
		}
	}
	return nil
}

// runSpanU — спана run'а в целых микрометрах.
type runSpanU struct {
	from units.Distance
	to   units.Distance
}

// checkTrackType проверяет форму одного типа: id и величины в правдоподобных
// диапазонах. Проверка !(v >= min && v <= max) отвергает и NaN, и бесконечность
// наравне с выходом за границы — валидатор не обязан полагаться на checkFinite.
func checkTrackType(prefix string, t *TrackType) error {
	if err := checkEntity("тип решётки", t.Name, t.ID); err != nil {
		return fmt.Errorf("%s%w", prefix, err)
	}
	bad := func(what string, v, min, max float64) error {
		return fmt.Errorf("%sтип %q: %s %g вне [%g, %g]", prefix, Labeled(t.Name, t.ID), what, v, min, max)
	}
	if !(t.Gauge >= MinGauge && t.Gauge <= MaxGauge) {
		return bad("gauge", t.Gauge, MinGauge, MaxGauge)
	}
	if !(t.Sleeper.Pitch >= MinSleeperPitch && t.Sleeper.Pitch <= MaxSleeperPitch) {
		return bad("sleeper.pitch", t.Sleeper.Pitch, MinSleeperPitch, MaxSleeperPitch)
	}
	if !(t.Sleeper.Length >= MinSleeperLength && t.Sleeper.Length <= MaxSleeperLength) {
		return bad("sleeper.length", t.Sleeper.Length, MinSleeperLength, MaxSleeperLength)
	}
	if !(t.Sleeper.Width >= MinSleeperWidth && t.Sleeper.Width <= MaxSleeperWidth) {
		return bad("sleeper.width", t.Sleeper.Width, MinSleeperWidth, MaxSleeperWidth)
	}
	if !(t.Ballast.HalfWidth >= MinBallastHalfWidth && t.Ballast.HalfWidth <= MaxBallastHalfWidth) {
		return bad("ballast.half_width", t.Ballast.HalfWidth, MinBallastHalfWidth, MaxBallastHalfWidth)
	}

	// Вертикальный стек (редакция 6 §3). Обязательность обеспечивается
	// ДИАПАЗОНОМ: пропущенное поле — ноль, ноль вне [min, max], карта
	// отвергнута. Отдельной проверки «поле задано» не заводится — две проверки
	// одного рано или поздно разойдутся.
	if !(t.Rail.Height >= MinRailHeight && t.Rail.Height <= MaxRailHeight) {
		return bad("rail.height", t.Rail.Height, MinRailHeight, MaxRailHeight)
	}
	if !(t.Rail.HeadWidth >= MinRailHeadWidth && t.Rail.HeadWidth <= MaxRailHeadWidth) {
		return bad("rail.head_width", t.Rail.HeadWidth, MinRailHeadWidth, MaxRailHeadWidth)
	}
	if err := checkRailSection(t); err != nil {
		return fmt.Errorf("%sтип %q: %w", prefix, Labeled(t.Name, t.ID), err)
	}
	if !(t.Sleeper.Height >= MinSleeperHeight && t.Sleeper.Height <= MaxSleeperHeight) {
		return bad("sleeper.height", t.Sleeper.Height, MinSleeperHeight, MaxSleeperHeight)
	}
	if !(t.Ballast.Depth >= MinBallastDepth && t.Ballast.Depth <= MaxBallastDepth) {
		return bad("ballast.depth", t.Ballast.Depth, MinBallastDepth, MaxBallastDepth)
	}
	if !(t.Ballast.SideSlope >= MinBallastSideSlope && t.Ballast.SideSlope <= MaxBallastSideSlope) {
		return bad("ballast.side_slope", t.Ballast.SideSlope, MinBallastSideSlope, MaxBallastSideSlope)
	}
	// CribDepth — единственная величина стека, чья граница не константа:
	// засыпать ящик выше верха шпалы значит закопать её, а ниже постели —
	// отрицательная засыпка. Ноль законен (призма вровень с постелью), поэтому
	// нижняя граница включающая, и «поле не задано» здесь НЕ отличается от
	// «задан ноль» — намеренно: это единственное поле стека, у которого ноль
	// осмыслен.
	if !(t.Ballast.CribDepth >= 0 && t.Ballast.CribDepth <= t.Sleeper.Height) {
		return fmt.Errorf(
			"%sтип %q: ballast.crib_depth %g вне [0, sleeper.height = %g]: шпальный ящик нельзя засыпать выше верха шпалы",
			prefix, t.ID, t.Ballast.CribDepth, t.Sleeper.Height)
	}

	// Брусья: блок необязателен, но заданный проверяется целиком. «Есть блок с
	// нулями» отвергается диапазоном, как и весь вертикальный стек выше.
	if t.Timber != nil {
		tb := t.Timber
		if !(tb.Pitch >= MinTimberPitch && tb.Pitch <= MaxTimberPitch) {
			return bad("timber.pitch", tb.Pitch, MinTimberPitch, MaxTimberPitch)
		}
		if !(tb.LengthMax >= MinTimberLength && tb.LengthMax <= MaxTimberLength) {
			return bad("timber.length_max", tb.LengthMax, MinTimberLength, MaxTimberLength)
		}
		if !(tb.Width >= MinTimberWidth && tb.Width <= MaxTimberWidth) {
			return bad("timber.width", tb.Width, MinTimberWidth, MaxTimberWidth)
		}
		if !(tb.Height >= MinTimberHeight && tb.Height <= MaxTimberHeight) {
			return bad("timber.height", tb.Height, MinTimberHeight, MaxTimberHeight)
		}
		// Самый длинный брус короче шпалы — это не перевод, а описка: брус несёт
		// ОБА пути и по построению не может быть короче шпалы одного из них.
		// Отдельным отказом, а не общим диапазоном, потому что общий диапазон тут
		// молчал бы: 2.0 м законны и для бруса, и для шпалы порознь.
		if tb.LengthMax < t.Sleeper.Length {
			return fmt.Errorf(
				"%sтип %q: timber.length_max %g короче sleeper.length %g — брус перекрывает оба пути и короче шпалы быть не может",
				prefix, Labeled(t.Name, t.ID), tb.LengthMax, t.Sleeper.Length)
		}
	}
	return nil
}

// FormationToRailTop — расстояние от верха основной площадки до поверхности
// катания. ПРОИЗВОДНАЯ величина, и это решение, а не удобство (редакция 6 §3.2).
//
// В карте её нет: авторское поле рядом со слагаемыми — второй источник истины,
// допуск согласования и вопрос «какое из двух верно» при расхождении. В проводе
// она есть, потому что провод производен, автора у него нет, и разойтись с
// собой он не может; зато сложение, выполненное клиентом и рельефом по
// отдельности, разойдётся округлением.
func (t TrackType) FormationToRailTop() float64 {
	return t.Ballast.Depth + t.Sleeper.Height + t.Rail.Height
}

// elementDomains возвращает домены [0, U] всех линейных элементов (рёбер и
// проходов стрелок) в целых микрометрах. Длина считается в единственном месте —
// horizontalLengthU: два независимых расчёта одного числа рано или поздно
// разойдутся.
func (m *Map) elementDomains() (map[string]units.Distance, error) {
	all := m.AllAlignments()
	out := make(map[string]units.Distance, len(all))
	for id, a := range all {
		u, err := horizontalLengthU(a)
		if err != nil {
			return nil, fmt.Errorf("элемент %q: %w", id, err)
		}
		out[id] = u
	}
	return out, nil
}

// passageIDs возвращает ID всех проходов стрелок. Проходы run'ами не
// покрываются — их решётка нерегулярна (спека §4).
func (m *Map) passageIDs() map[string]bool {
	out := make(map[string]bool, 2*len(m.Topology.Turnouts))
	for _, t := range m.Topology.Turnouts {
		out[t.ID+PassageStraight] = true
		out[t.ID+PassageDiverging] = true
	}
	return out
}

// Диапазоны размеров платформы (спека §3). Проверка знака недостаточна: ширина
// 0.001 м проходит «строго положительно», но платформа такой ширины не рисуется
// никак, а умолчание в клиенте — ровно то, что спека убирает. Границы —
// правдоподобные пределы пассажирских платформ, метры.
const (
	MinPlatformOffset, MaxPlatformOffset = 1.0, 4.0  // от оси пути до ближней кромки: ближе метра — габарит, дальше четырёх — уже не к этому пути
	MinPlatformWidth, MaxPlatformWidth   = 1.0, 12.0 // поперёк: реальные пассажирские платформы 2–12 м
)

// checkPlatformSizes проверяет размеры сооружений, несущих габарит: платформы и
// упора. Обязательность обеспечивается диапазоном: пропущенное поле — ноль, а
// ноль вне [min, max]. Проверка !(v >= min && v <= max) отвергает и NaN, и
// бесконечность наравне с выходом за границы — валидатор не обязан полагаться на
// checkFinite.
//
// Имя осталось платформенным, хотя проверяется уже не только платформа: упор
// получил габарит редакцией 6 §4.2 и попал сюда же, потому что довод один —
// сооружение без размеров не рисуется никак, а умолчание в клиенте запрещено.
// Мост и тоннель размеров по-прежнему не несут: их геометрия — отдельный слой
// (map-content-design §9а), и заводить им ширину сейчас значило бы объявить
// форму без исполнителя.
func (m *Map) checkPlatformSizes(prefix string) error {
	bad := func(what, id, field string, v, min, max float64) error {
		return fmt.Errorf("%s%s %q: %s %g вне [%g, %g]", prefix, what, id, field, v, min, max)
	}
	for i := range m.Topology.Structures {
		st := &m.Topology.Structures[i]
		switch st.Kind {
		case "platform":
			if !(st.Offset >= MinPlatformOffset && st.Offset <= MaxPlatformOffset) {
				return bad("платформа", st.ID, "offset", st.Offset, MinPlatformOffset, MaxPlatformOffset)
			}
			if !(st.Width >= MinPlatformWidth && st.Width <= MaxPlatformWidth) {
				return bad("платформа", st.ID, "width", st.Width, MinPlatformWidth, MaxPlatformWidth)
			}
			// Вертикаль платформы (редакция 6 §4.1). Height — НАД ПОВЕРХНОСТЬЮ
			// КАТАНИЯ: до объявления датума это число было бессмысленным, и
			// потому его не было.
			if !(st.Height >= MinPlatformHeight && st.Height <= MaxPlatformHeight) {
				return bad("платформа", st.ID, "height", st.Height, MinPlatformHeight, MaxPlatformHeight)
			}
			if !(st.SlabThickness >= MinPlatformSlabThick && st.SlabThickness <= MaxPlatformSlabThick) {
				return bad("платформа", st.ID, "slab_thickness", st.SlabThickness, MinPlatformSlabThick, MaxPlatformSlabThick)
			}
		case "buffer_stop":
			if !(st.Height >= MinBufferStopHeight && st.Height <= MaxBufferStopHeight) {
				return bad("упор", st.ID, "height", st.Height, MinBufferStopHeight, MaxBufferStopHeight)
			}
			if !(st.Width >= MinBufferStopWidth && st.Width <= MaxBufferStopWidth) {
				return bad("упор", st.ID, "width", st.Width, MinBufferStopWidth, MaxBufferStopWidth)
			}
		}
	}
	return nil
}

// checkRailSection — СЕЧЕНИЕ РЕЛЬСА: замкнутый простой многоугольник, обойдённый
// против часовой стрелки, согласованный с высотой и шириной головки.
//
// # Почему проверок пять, а не одна
//
// Каждая ловит СВОЮ ошибку автора, и каждая из этих ошибок доезжает до кадра в
// виде исправного на вид рельса:
//
//	мало точек          — сечения нет вовсе, рисовать нечего;
//	обход по часовой    — рельс выворачивается наизнанку (тот же класс, что
//	                      коробки домов 2026-08-12, найденные глазами);
//	y > 0               — металл над поверхностью катания, то есть колесо
//	                      катится по воздуху;
//	глубина ≠ height    — два источника высоты рельса разошлись, и вертикальный
//	                      стек (formation_to_rail_top) посчитан по одному из них;
//	ширина верха ≠ head_width — рабочая грань не там, где её объявили, и колея
//	                      поехала (замер спайка: 1.335 вместо 1.435).
//
// Ни одна из них НЕ ЧИНИТСЯ подстановкой: карта, где автор ошибся в сечении,
// обязана получить отказ, а не правдоподобный рельс.
func checkRailSection(t *TrackType) error {
	s := t.Rail.Section
	// СЕЧЕНИЕ НЕОБЯЗАТЕЛЬНО, и это не то умолчание, которое проект запрещает.
	//
	// Запрещено молчаливое ПРАВДОПОДОБНОЕ ЗАМЕЩЕНИЕ: карта без обязательного
	// поля не должна получать выдуманное. Здесь замещения нет — есть ОБЪЯВЛЕННОЕ
	// УПРОЩЕНИЕ, прожившее в контракте с редакции 6: рельс без сечения рисуется
	// прямоугольником head_width × height, и упрощение названо вслух с обеих
	// сторон провода. Клиент своего Р65 не подставляет.
	//
	// Обязательным сечение станет тогда же, когда исчезнет прямоугольник, — то
	// есть когда у КАЖДОЙ карты он будет, и отказ перестанет означать «этот
	// автор ещё не дошёл до профиля».
	if len(s) == 0 {
		return nil
	}
	if len(s) < MinRailSectionPoints {
		return fmt.Errorf("rail.section: точек %d, нужно хотя бы %d — сечения нет",
			len(s), MinRailSectionPoints)
	}
	if len(s) > MaxRailSectionPoints {
		return fmt.Errorf("rail.section: точек %d, потолок %d", len(s), MaxRailSectionPoints)
	}
	// Знак площади — он же направление обхода. Считается по формуле шнурков;
	// вырожденное сечение (площадь около нуля) отвергается тем же неравенством.
	area := 0.0
	for i := range s {
		j := (i + 1) % len(s)
		area += s[i].X()*s[j].Y() - s[j].X()*s[i].Y()
	}
	area /= 2
	if area < MinRailSectionArea {
		return fmt.Errorf(
			"rail.section: площадь %+.6f м² — обход по часовой стрелке либо сечение вырождено; "+
				"нужен обход против часовой в осях (x наружу, y вверх), иначе рельс выйдет вывернутым",
			area)
	}
	depth := 0.0
	// Головка — это точки, лежащие НА ПОВЕРХНОСТИ КАТАНИЯ. Их крайние x дают и
	// ширину головки, и место рабочей грани; обе величины считаются от них, а не
	// от габарита всего сечения, — подошва шире головки, и габарит соврал бы.
	topMin, topMax := 0.0, 0.0
	haveTop := false
	for _, p := range s {
		if p.Y() > MaxRailSectionY {
			return fmt.Errorf(
				"rail.section: точка (%g, %g) выше поверхности катания — y отсчитывается ОТ НЕЁ вниз и положительным не бывает",
				p.X(), p.Y())
		}
		if -p.Y() > depth {
			depth = -p.Y()
		}
		if p.Y() >= MaxRailSectionY-RailSectionTol {
			if !haveTop || p.X() < topMin {
				topMin = p.X()
			}
			if !haveTop || p.X() > topMax {
				topMax = p.X()
			}
			haveTop = true
		}
	}
	if !haveTop {
		return fmt.Errorf(
			"rail.section: ни одна точка не лежит на поверхности катания — головки у такого рельса нет, и колесу катиться не по чему")
	}
	if math.Abs(depth-t.Rail.Height) > RailSectionTol {
		return fmt.Errorf(
			"rail.section: сечение глубиной %g м при rail.height %g м — высота рельса объявлена дважды и разошлась",
			depth, t.Rail.Height)
	}
	if math.Abs(topMax-topMin-t.Rail.HeadWidth) > RailSectionTol {
		return fmt.Errorf(
			"rail.section: головка поверху шириной %g м при rail.head_width %g м — "+
				"рабочая грань не там, где объявлена, и колея поедет",
			topMax-topMin, t.Rail.HeadWidth)
	}
	// РАБОЧАЯ ГРАНЬ — НАЧАЛО ОТСЧЁТА, и хоть одна точка обязана на ней лежать:
	// сечение, целиком сдвинутое наружу, встало бы мимо колеи, не нарушив ни
	// высоты, ни ширины головки.
	if math.Abs(topMin) > RailSectionTol {
		return fmt.Errorf(
			"rail.section: головка начинается на x = %g м, а не на нуле — "+
				"x отсчитывается ОТ РАБОЧЕЙ ГРАНИ, и она же начало отсчёта",
			topMin)
	}
	return nil
}

// validateTrackFrog проверяет КРЕСТОВИННЫЙ КОМПЛЕКТ.
//
// Вынесено из проверки типа пути 2026-08-16, когда комплект переехал в каталог
// типов устройств: правила у него не изменились, изменился владелец. Второй
// копии этих порогов проект не заводит — они и здесь одни.
func validateTrackFrog(where string, f TrackFrog) error {
	bad := func(field string, got, lo, hi float64) error {
		return fmt.Errorf("mapfmt: тип устройства %s: %s = %v вне [%v, %v]", where, field, got, lo, hi)
	}
	if !(f.Flangeway >= MinFlangeway && f.Flangeway <= MaxFlangeway) {
		return bad("frog_set.flangeway", f.Flangeway, MinFlangeway, MaxFlangeway)
	}
	if !(f.CheckFlangeway >= MinFlangeway && f.CheckFlangeway <= MaxFlangeway) {
		return bad("frog_set.check_flangeway", f.CheckFlangeway, MinFlangeway, MaxFlangeway)
	}
	if !(f.WingLength >= MinFrogRailLength && f.WingLength <= MaxFrogRailLength) {
		return bad("frog_set.wing_length", f.WingLength, MinFrogRailLength, MaxFrogRailLength)
	}
	if !(f.CheckLength >= MinFrogRailLength && f.CheckLength <= MaxFrogRailLength) {
		return bad("frog_set.check_length", f.CheckLength, MinFrogRailLength, MaxFrogRailLength)
	}
	if !(f.CastingLength >= MinFrogRailLength && f.CastingLength <= MaxFrogRailLength) {
		return bad("frog_set.casting_length", f.CastingLength, MinFrogRailLength, MaxFrogRailLength)
	}
	// СЕРДЕЧНИК КОРОЧЕ УСОВИКА. Усовик обнимает сердечник с двух сторон и по
	// построению длиннее его; отливка длиннее своих крыльев — это уже не
	// крестовина, а вставка.
	if f.CastingLength*2 > f.WingLength {
		return fmt.Errorf("mapfmt: тип устройства %s: сердечник %g в каждую сторону не помещается в усовик %g — крылья короче отливки",
			where, f.CastingLength, f.WingLength)
	}
	if !(f.Flare >= MinFlare && f.Flare <= MaxFlare) {
		return bad("frog_set.flare", f.Flare, MinFlare, MaxFlare)
	}
	if f.FlareGap <= f.Flangeway {
		return fmt.Errorf("mapfmt: тип устройства %s: раструб %g не шире рабочего желоба %g — раструб на то и раструб",
			where, f.FlareGap, f.Flangeway)
	}
	// ДВА ОТГИБА КОРОЧЕ САМОЙ НИТКИ: нитка из одних отгибов ничего не удерживает.
	if 2*f.Flare >= f.CheckLength || 2*f.Flare >= f.WingLength {
		return fmt.Errorf("mapfmt: тип устройства %s: два отгиба по %g не помещаются в усовик %g и контррельс %g — рабочей части не остаётся",
			where, f.Flare, f.WingLength, f.CheckLength)
	}
	return nil
}
