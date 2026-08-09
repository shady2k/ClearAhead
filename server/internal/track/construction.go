package track

import (
	"fmt"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// fillConstruction переносит блок construction карты в провод (спека §3–4):
// типы, run'ы с разрешённым умолчанием и версию алгоритма размещения.
//
// Списки сортируются канонически — типы по id, run'ы по id: обход Go-map
// недетерминирован, а хеш геометрии считается по отдаваемым байтам. Спаны
// внутри run остаются в авторском порядке: он и есть смысл run'а, сортировке
// не подлежит (спека §10).
//
// Компилятор разрешает умолчание: у run'а с опущенным type берётся
// construction.default_type. В проводе ссылка всегда явная — клиент скрытого
// умолчания не применяет никогда (спека §4). Блок отсутствует — рецепта нет,
// в проводе пустые массивы.
func fillConstruction(m *mapfmt.Map, rg *RenderGeometry) error {
	c := m.Construction
	if c == nil {
		return nil
	}

	byID := make(map[string]mapfmt.TrackType, len(c.Types))
	ids := make([]string, 0, len(c.Types))
	for i := range c.Types {
		t := c.Types[i]
		byID[t.ID] = t
		ids = append(ids, t.ID)
	}
	sort.Strings(ids)
	for _, id := range ids {
		t := byID[id]
		rg.TrackTypes = append(rg.TrackTypes, RenderTrackType{
			ID:    t.ID,
			Gauge: t.Gauge,
			Sleeper: RenderSleeper{
				Pitch:  t.Sleeper.Pitch,
				Length: t.Sleeper.Length,
				Width:  t.Sleeper.Width,
			},
			Ballast: RenderBallast{HalfWidth: t.Ballast.HalfWidth},
		})
	}

	runs := make(map[string]mapfmt.ConstructionRun, len(c.Runs))
	runIDs := make([]string, 0, len(c.Runs))
	for i := range c.Runs {
		r := c.Runs[i]
		runs[r.ID] = r
		runIDs = append(runIDs, r.ID)
	}
	sort.Strings(runIDs)
	for _, id := range runIDs {
		r := runs[id]
		typ := r.Type
		if typ == "" {
			typ = c.DefaultType
		}
		if _, ok := byID[typ]; !ok {
			return fmt.Errorf("track: run %s: тип %q не разрешается", r.ID, typ)
		}
		rr := RenderRun{
			ID:         r.ID,
			Type:       typ,
			Coordinate: r.Coordinate,
			Phase:      r.Phase,
			Spans:      make([]RenderRunSpan, 0, len(r.Spans)),
		}
		for _, sp := range r.Spans {
			rr.Spans = append(rr.Spans, RenderRunSpan{
				Element:   sp.Element,
				From:      sp.From,
				To:        sp.To,
				Direction: sp.Direction,
			})
		}
		rg.ConstructionRuns = append(rg.ConstructionRuns, rr)
	}
	return nil
}
