package track

import (
	"fmt"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
)

// fillConstruction переносит блок construction карты в провод (спека §3–4):
// типы, run'ы с разрешённым умолчанием и версию алгоритма размещения.
//
// Списки сортируются канонически — типы по id, run'ы по id: обход Go-map
// недетерминирован, а network_hash считается по отдаваемым байтам. Спаны
// внутри run остаются в авторском порядке: он и есть смысл run'а, сортировке
// не подлежит (спека §10).
//
// Компилятор разрешает умолчание: у run'а с опущенным type берётся
// construction.default_type. В проводе ссылка всегда явная — клиент скрытого
// умолчания не применяет никогда (спека §4). Блок отсутствует — рецепта нет,
// в проводе пустые массивы.
// renderRail переносит профиль рельса в провод.
//
// Заведена, когда у остряка появился свой профиль ОР65: путевой рельс и
// остряковый описываются одним и тем же, и два места, копирующие их порознь,
// разошлись бы при первой правке формы.
func renderRail(r mapfmt.TrackRail) RenderRail {
	return RenderRail{
		Height:    r.Height,
		HeadWidth: r.HeadWidth,
		// СЕЧЕНИЕ КОПИРУЕТСЯ, А НЕ ОТДАЁТСЯ ССЫЛКОЙ на срез карты: контракт живёт
		// дольше разбора, и общий массив дал бы двум сторонам одну память.
		Section: railSection(r.Section),
	}
}

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
			Name:  t.Name,
			Gauge: t.Gauge,
			Rail: RenderRail{
				Height:    t.Rail.Height,
				HeadWidth: t.Rail.HeadWidth,
				// СЕЧЕНИЕ КОПИРУЕТСЯ, А НЕ ОТДАЁТСЯ ССЫЛКОЙ на срез карты: контракт
				// живёт дольше разбора, и общий массив дал бы двум сторонам одну
				// память.
				Section: railSection(t.Rail.Section),
			},
			Sleeper: RenderSleeper{
				Pitch:  t.Sleeper.Pitch,
				Length: t.Sleeper.Length,
				Width:  t.Sleeper.Width,
				Height: t.Sleeper.Height,
			},
			Ballast: RenderBallast{
				HalfWidth: t.Ballast.HalfWidth,
				Depth:     t.Ballast.Depth,
				CribDepth: t.Ballast.CribDepth,
				SideSlope: t.Ballast.SideSlope,
			},
			// Сумма считается ЗДЕСЬ и один раз: см. RenderTrackType.
			// FormationToRailTop — почему производное поле едет в провод, хотя
			// в карте его нет.
			FormationToRailTop: t.FormationToRailTop(),
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
			return fmt.Errorf("track: run %s: тип %q не разрешается", mapfmt.Labeled(r.Name, r.ID), typ)
		}
		rr := RenderRun{
			ID:         r.ID,
			Name:       r.Name,
			Type:       typ,
			Coordinate: r.Coordinate,
			Phase:      r.Phase,
			Spans:      make(netloc.LinearU, 0, len(r.Spans)),
		}
		rr.Spans = append(rr.Spans, r.Spans...)
		rg.ConstructionRuns = append(rg.ConstructionRuns, rr)
	}
	return nil
}

// railSection — сечение рельса в провод.
//
// Пары чисел, а не структуры с именами полей: у сечения дюжина вершин, и на
// проводе таблица пар и читается, и весит меньше. Оси и датумы объявлены один
// раз — у mapfmt.TrackRail.Section, и здесь не повторяются.
func railSection(src []mapfmt.SectionPoint) [][2]float64 {
	if len(src) == 0 {
		return nil
	}
	out := make([][2]float64, len(src))
	for i, p := range src {
		out[i] = [2]float64{p.X(), p.Y()}
	}
	return out
}
