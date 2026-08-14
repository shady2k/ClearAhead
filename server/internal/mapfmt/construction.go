package mapfmt

import (
	"fmt"
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
	const prefix = "отрисовка: "
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
