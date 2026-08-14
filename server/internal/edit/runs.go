package edit

import (
	"fmt"
	"math"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/uuidv7"
)

// Run'ы путевой решётки — авторитетный факт о физической решётке, независимый
// от нарезки на элементы (требование 6): смена внутренней сегментации карты
// не должна переставлять шпалы. Сквозь стрелку решётка может течь непрерывно
// (пример — RUN_APPROACH_CROSS в fixture_station: подход и переезд — один run,
// спаны проходов в run'ы не попадают), поэтому run'ы не пересобираются из
// топологии — авторское решение о склейке было бы потеряно. Правка вносит в
// run'ы хирургические изменения ровно в месте правки:
//
//   - продление: новое ребро вливается в run ребра, заканчивавшегося в порту;
//   - ветвление: спад разрезанного ребра делится на два в том же направлении,
//     ветвь получает собственный run;
//   - стирка: спаны удалённых рёбер уходят, пустые run'ы исчезают;
//   - упор и платформа топологию не меняют — run'ы не трогаются.
//
// Новые run'ы получают фазу 0 и умолчательный тип (поле type опущено —
// умолчание разрешает компилятор).

// extendRuns вливает новое ребро newEdge (начинается в порту port) в run ребра,
// которое заканчивалось в этом порту. Обычный случай — run заканчивается у
// порта: спад дописывается, решётка шпал продолжается через стык как была.
// Если run, наоборот, начинался у порта (физический путь тёк от порта), run
// разворачивается: продолжение получает новое начало, а фаза пересчитывается
// так, чтобы решётка шпал осталась той же физической решёткой.
func extendRuns(m *mapfmt.Map, runs []mapfmt.ConstructionRun, port, newEdge string) ([]mapfmt.ConstructionRun, error) {
	oldEdge := ""
	for _, e := range m.Topology.Edges {
		if e.To == port {
			oldEdge = e.ID
			break
		}
	}
	if oldEdge == "" {
		return nil, fmt.Errorf("ребро у порта %s не найдено", port)
	}
	span, err := newRunSpan(m, newEdge, "forward")
	if err != nil {
		return nil, err
	}

	out := make([]mapfmt.ConstructionRun, 0, len(runs))
	done := false
	for i := range runs {
		r := runs[i]
		idx := -1
		for j := range r.Spans {
			if r.Spans[j].Element == oldEdge {
				idx = j
			}
		}
		if idx < 0 {
			out = append(out, r)
			continue
		}
		if done {
			return nil, fmt.Errorf("ребро %s в двух run'ах", oldEdge)
		}
		done = true
		switch idx {
		case len(r.Spans) - 1: // run заканчивался у порта
			r.Spans = append(r.Spans, span)
		case 0: // run начинался у порта — разворачиваем
			r = reverseRun(m, r)
			r.Spans = append(r.Spans, span)
		default:
			return nil, fmt.Errorf("ребро %s в середине run'а — неожиданная структура", oldEdge)
		}
		out = append(out, r)
	}
	if !done {
		return nil, fmt.Errorf("ребро %s не покрыто ни одним run'ом", oldEdge)
	}
	return out, nil
}

// reverseRun разворачивает run: порядок спанов наоборот, направления
// перевёрнуты. Фаза пересчитывается так, чтобы решётка шпал осталась той же
// самой: координата точки связана со старой как g' = L − g, а фаза нового
// run'а — нормализованный по шагу шпал остаток (L − phase) mod pitch: фаза
// обязана лежать в [0, pitch) (валидатор), и именно эта нормализация
// оставляет шпалы на тех же физических местах.
//
// Спаны копируются: run принимается по значению, но разворот на месте
// мутировал бы общий с аргументом backing array — функция обязана вести
// себя чисто.
func reverseRun(m *mapfmt.Map, r mapfmt.ConstructionRun) mapfmt.ConstructionRun {
	var L float64
	for _, sp := range r.Spans {
		L += sp.To - sp.From
	}
	if pitch := runPitch(m, r); pitch > 0 {
		r.Phase = math.Mod(L-r.Phase, pitch)
		if r.Phase < 0 {
			r.Phase += pitch
		}
	}
	spans := make([]netloc.IntervalU, len(r.Spans))
	copy(spans, r.Spans)
	r.Spans = spans
	for i, j := 0, len(r.Spans)-1; i < j; i, j = i+1, j-1 {
		r.Spans[i], r.Spans[j] = r.Spans[j], r.Spans[i]
	}
	for i := range r.Spans {
		if r.Spans[i].Direction == "forward" {
			r.Spans[i].Direction = "reverse"
		} else {
			r.Spans[i].Direction = "forward"
		}
	}
	return r
}

// runPitch — шаг шпал разрешённого типа run'а (r.Type или умолчание карты),
// как у валидатора (mapfmt). reverseRun нужен m именно для этого: фаза после
// разворота нормализуется по шагу. 0 — тип не разрешился (карта невалидна).
func runPitch(m *mapfmt.Map, r mapfmt.ConstructionRun) float64 {
	if m == nil || m.Construction == nil {
		return 0
	}
	typ := r.Type
	if typ == "" {
		typ = m.Construction.DefaultType
	}
	for _, t := range m.Construction.Types {
		if t.ID == typ {
			return t.Sleeper.Pitch
		}
	}
	return 0
}

// splitRuns делит ребро edgeID в точке реза: голова [0, headLen] остаётся на
// прежнем ребре, хвост переходит на новое (tailID). Кумулятивная длина
// run'а в каждой физической точке не меняется — шпалы не переставляются.
func splitRuns(m *mapfmt.Map, runs []mapfmt.ConstructionRun, edgeID, tailID string) ([]mapfmt.ConstructionRun, error) {
	headLen, err := alignmentsLengthU(m.Geometry.Edges[edgeID])
	if err != nil {
		return nil, fmt.Errorf("голова ребра %s: %w", edgeID, err)
	}
	tailLen, err := alignmentsLengthU(m.Geometry.Edges[tailID])
	if err != nil {
		return nil, fmt.Errorf("хвост ребра %s: %w", tailID, err)
	}

	out := make([]mapfmt.ConstructionRun, 0, len(runs))
	done := false
	for i := range runs {
		r := runs[i]
		// Свежий срез: спаны пишем, пока читаем исходный — разделение на два
		// спана обгоняло бы читающий индекс при переиспользовании буфера.
		spans := make([]netloc.IntervalU, 0, len(r.Spans)+1)
		for _, sp := range r.Spans {
			if sp.Element != edgeID {
				spans = append(spans, sp)
				continue
			}
			if done {
				return nil, fmt.Errorf("ребро %s в двух run'ах", edgeID)
			}
			done = true
			head := netloc.IntervalU{Element: edgeID, From: 0, To: headLen.Meters(), Direction: sp.Direction}
			tail := netloc.IntervalU{Element: tailID, From: 0, To: tailLen.Meters(), Direction: sp.Direction}
			// Порядок зависит от направления: forward проходится от головы к
			// хвосту, reverse — от хвоста к голове (въезд с конца ребра).
			// Неверный порядок разрывает физическую непрерывность run'а и
			// сдвигает кумулятивную длину в точках.
			if sp.Direction == "reverse" {
				spans = append(spans, tail, head)
			} else {
				spans = append(spans, head, tail)
			}
		}
		r.Spans = spans
		out = append(out, r)
	}
	if !done {
		return nil, fmt.Errorf("ребро %s не покрыто ни одним run'ом", edgeID)
	}
	return out, nil
}

// dropRuns выкидывает спаны удалённых рёбер и пустые run'ы.
func dropRuns(runs []mapfmt.ConstructionRun, removed map[string]bool) []mapfmt.ConstructionRun {
	out := make([]mapfmt.ConstructionRun, 0, len(runs))
	for i := range runs {
		r := runs[i]
		spans := r.Spans[:0]
		for _, sp := range r.Spans {
			if !removed[sp.Element] {
				spans = append(spans, sp)
			}
		}
		if len(spans) == 0 {
			continue
		}
		r.Spans = spans
		out = append(out, r)
	}
	return out
}

// newRunForEdge — свежий run для нового ребра: фаза 0, направление forward.
// Тождество run'а — UUIDv7 из источника, метка — RUN_<метка ребра> (решение
// владельца 2026-08-13 «UUIDv7 везде»: прежний составной ID «RUN_+edgeID»
// больше не тождество — тождество UUID, имя отдельным полем).
func newRunForEdge(m *mapfmt.Map, ids uuidv7.Source, edgeID string) (mapfmt.ConstructionRun, error) {
	sp, err := newRunSpan(m, edgeID, "forward")
	if err != nil {
		return mapfmt.ConstructionRun{}, err
	}
	name := ""
	for _, e := range m.Topology.Edges {
		if e.ID == edgeID {
			name = "RUN_" + e.Name
			break
		}
	}
	id, err := allocID(m, ids, name)
	if err != nil {
		return mapfmt.ConstructionRun{}, err
	}
	return mapfmt.ConstructionRun{
		ID:         id,
		Name:       name,
		Coordinate: "u",
		Phase:      0,
		Spans:      []netloc.IntervalU{sp},
	}, nil
}

// newRunSpan — спад нового ребра на всю его длину.
func newRunSpan(m *mapfmt.Map, edgeID string, direction netloc.Direction) (netloc.IntervalU, error) {
	u, err := alignmentsLengthU(m.Geometry.Edges[edgeID])
	if err != nil {
		return netloc.IntervalU{}, fmt.Errorf("длина ребра %s: %w", edgeID, err)
	}
	return netloc.IntervalU{Element: edgeID, From: 0, To: u.Meters(), Direction: direction}, nil
}
