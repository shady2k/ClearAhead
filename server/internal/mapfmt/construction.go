package mapfmt

import (
	"fmt"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/units"
)

// validateConstruction — модуль «отрисовка»: типы путевых конструкций, run'ы
// размещения и покрытие рёбер (спека контракта отрисовки §3–4, план волны 2a,
// задачи 1–2).
//
// Блок construction отсутствует — решётки нет, рецепта нет: модуль пропускает
// карту. Это сознательное решение, а не умолчание по недосмотру: блок порождает
// строитель (авторинг), и карта, где решётка ещё не авторилась, законна — в
// проводе ей соответствуют пустые track_types и construction_runs. Присутствует
// блок — проверяется полностью: форма, ссылки, диапазоны, покрытие.
//
// Каждый отказ начинается с префикса «отрисовка: » — модуль называет себя в
// тексте отказа (см. Validate).
func (m *Map) validateConstruction() error {
	const prefix = "отрисовка: "
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
		if r.ID == "" || len(r.ID) > MaxIDLength {
			return fmt.Errorf("%srun без id или id длиннее %d", prefix, MaxIDLength)
		}
		if seenRuns[r.ID] {
			return fmt.Errorf("%srun %q объявлен дважды", prefix, r.ID)
		}
		seenRuns[r.ID] = true
		typ := r.Type
		if typ == "" {
			typ = c.DefaultType
		}
		if _, ok := types[typ]; !ok {
			return fmt.Errorf("%srun %q: неизвестный тип %q", prefix, r.ID, typ)
		}
		if r.Coordinate != "u" {
			return fmt.Errorf("%srun %q: coordinate %q, разрешено только \"u\"", prefix, r.ID, r.Coordinate)
		}
		// Фаза нормализована относительно шага РАЗРЕШЁННОГО типа run'а:
		// полуоткрытое правило размещения phase + n·pitch ∈ [0, run_length).
		pitch := types[typ].Sleeper.Pitch
		if !(r.Phase >= 0 && r.Phase < pitch) {
			return fmt.Errorf("%srun %q: phase %g вне [0, pitch=%g)", prefix, r.ID, r.Phase, pitch)
		}
		if len(r.Spans) == 0 {
			return fmt.Errorf("%srun %q: нет ни одного спана", prefix, r.ID)
		}
		if len(r.Spans) > MaxRunSpans {
			return fmt.Errorf("%srun %q: спанов больше %d", prefix, r.ID, MaxRunSpans)
		}
		for j := range r.Spans {
			sp := &r.Spans[j]
			if sp.Direction != "forward" && sp.Direction != "reverse" {
				return fmt.Errorf("%srun %q: спана %d: направление %q, ожидается forward или reverse",
					prefix, r.ID, j, sp.Direction)
			}
			dom, ok := domains[sp.Element]
			if !ok {
				return fmt.Errorf("%srun %q: спана %d: элемент %q не существует", prefix, r.ID, j, sp.Element)
			}
			if passages[sp.Element] {
				return fmt.Errorf("%srun %q: спана %d покрывает проход устройства %q — решётка устройств нерегулярна, run'ы её не покрывают",
					prefix, r.ID, j, sp.Element)
			}
			from, err := units.MetersToDistance(sp.From)
			if err != nil {
				return fmt.Errorf("%srun %q: спана %d: %w", prefix, r.ID, j, err)
			}
			to, err := units.MetersToDistance(sp.To)
			if err != nil {
				return fmt.Errorf("%srun %q: спана %d: %w", prefix, r.ID, j, err)
			}
			if !(from >= 0 && to > from) {
				return fmt.Errorf("%srun %q: спана %d: интервал [%g, %g] вырожден",
					prefix, r.ID, j, sp.From, sp.To)
			}
			if to > dom {
				return fmt.Errorf("%srun %q: спана %d: конец %g за пределами элемента %q (длина %s)",
					prefix, r.ID, j, sp.To, sp.Element, dom)
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
			return fmt.Errorf("%sребро %q не покрыто ни одним run", prefix, e.ID)
		}
		sort.Slice(spans, func(i, j int) bool { return spans[i].from < spans[j].from })
		if spans[0].from > abutTolerance {
			return fmt.Errorf("%sребро %q: покрытие начинается с %s, ожидается 0", prefix, e.ID, spans[0].from)
		}
		u := domains[e.ID]
		if u-spans[len(spans)-1].to > abutTolerance {
			return fmt.Errorf("%sребро %q: покрытие кончается на %s, ожидается %s",
				prefix, e.ID, spans[len(spans)-1].to, u)
		}
		for i := 1; i < len(spans); i++ {
			gap := spans[i].from - spans[i-1].to
			if gap < -abutTolerance {
				return fmt.Errorf("%sребро %q: перекрытие спанов %s–%s и %s–%s",
					prefix, e.ID, spans[i-1].from, spans[i-1].to, spans[i].from, spans[i].to)
			}
			if gap > abutTolerance {
				return fmt.Errorf("%sребро %q: пропуск между спанами %s и %s",
					prefix, e.ID, spans[i-1].to, spans[i].from)
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
	if t.ID == "" || len(t.ID) > MaxIDLength {
		return fmt.Errorf("%sтип без id или id длиннее %d", prefix, MaxIDLength)
	}
	bad := func(what string, v, min, max float64) error {
		return fmt.Errorf("%sтип %q: %s %g вне [%g, %g]", prefix, t.ID, what, v, min, max)
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
	return nil
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
