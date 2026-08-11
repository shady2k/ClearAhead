// Package worldgen — конвейер входа: карта попадает в базу мира.
//
// # Инвариант входа
//
// В базу попадает только то, что прошло ПОЛНЫЙ путь входа: разбор, валидацию,
// компиляцию. Правило mapstore «файл, который нельзя загрузить, не пишется»
// переносится сюда дословно (`world-storage` §6). Поэтому Generate сам зовёт
// валидацию и распространение поз, а не доверяет вызывающему.
//
// # Порядок
//
//	карта -> валидация -> распространение поз -> рельеф
//	                                               |
//	                        выбор чанков по удалённости от пути
//	                                               |
//	                                развёртка в отсчёты -> база
//
// # JSON не выбрасывается
//
// Он меняет роль: остаётся обменным и авторским форматом — импорт, экспорт,
// дифф, фикстуры, ручное авторство небольшой станции. Рантайм читает базу.
package worldgen

import (
	"fmt"
	"math"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/track"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// Report — что получилось. Возвращается вызывающему, а не пишется в лог:
// решать, показывать это человеку или нет, не дело конвейера.
type Report struct {
	Region      string
	ByLevel     map[int]int
	TotalChunks int
	TotalBytes  int
}

// Generate разворачивает карту в чанки и пишет их в базу.
//
// Регион должен быть заведён заранее: у него своя геопривязка и своё
// происхождение, и придумывать их за автора конвейер не вправе.
func Generate(s *worldstore.Store, m *mapfmt.Map, region string, revision int64) (Report, error) {
	rep := Report{Region: region, ByLevel: map[int]int{}}

	if _, ok, err := s.GetRegion(region); err != nil {
		return rep, err
	} else if !ok {
		return rep, fmt.Errorf("worldgen: региона %s нет в базе; заведите его до порождения чанков", region)
	}

	// Полный путь входа. Валидация зовётся здесь, а не предполагается
	// сделанной: инвариант базы держится этим вызовом.
	if err := mapfmt.Validate(m); err != nil {
		return rep, fmt.Errorf("worldgen: карта не прошла валидацию: %w", err)
	}
	_, els, err := track.Propagate(m)
	if err != nil {
		return rep, fmt.Errorf("worldgen: распространение поз: %w", err)
	}
	if m.Terrain == nil {
		return rep, fmt.Errorf("worldgen: у карты нет рельефа; порождать нечего")
	}
	field, err := terrain.New(m, els)
	if err != nil {
		return rep, fmt.Errorf("worldgen: рельеф: %w", err)
	}

	minX, minY, maxX, maxY, ok := field.Bounds()
	if !ok {
		return rep, fmt.Errorf("worldgen: у карты нет ни одной точки оси")
	}

	baseZmm := int64(math.Round(field.BaseZ() * 1000))
	// Охват: габарит оси, расширенный на предельный радиус последнего уровня.
	// Дальше него LevelFor не отдаёт ни одного уровня, и перебирать нечего.
	reach := chunk.Level0RadiusM * math.Pow(2, chunk.MaxLevel)

	for level := 0; level <= chunk.MaxLevel; level++ {
		side := chunk.SideM(level)
		c0x := int(math.Floor((minX - reach) / side))
		c1x := int(math.Floor((maxX + reach) / side))
		c0y := int(math.Floor((minY - reach) / side))
		c1y := int(math.Floor((maxY + reach) / side))

		for cx := c0x; cx <= c1x; cx++ {
			for cz := c0y; cz <= c1y; cz++ {
				a := chunk.Address{Region: region, Level: level, CX: cx, CZ: cz}
				// Уровень выбирается по ЦЕНТРУ чанка. Так каждый чанк получает
				// ровно один уровень: полосы удалённости не пересекаются и не
				// оставляют пропусков. Ближе к границе полосы это даёт чанк
				// уровнем грубее или мельче ожидаемого — цена принята
				// сознательно взамен перекрытий.
				cxm, czm := a.OriginM()
				d, hit := field.DistanceToAxis(cxm+side/2, czm+side/2)
				if !hit {
					continue
				}
				want, ok := chunk.LevelFor(d)
				if !ok || want != level {
					continue
				}
				heights, err := field.ChunkHeights(a)
				if err != nil {
					return rep, err
				}
				err = s.PutChunk(worldstore.Chunk{
					Address:  a,
					Revision: revision,
					BaseZmm:  baseZmm,
					Heights:  heights,
				})
				if err != nil {
					return rep, err
				}
				rep.ByLevel[level]++
				rep.TotalChunks++
				rep.TotalBytes += chunk.HeightsBytes
			}
		}
	}
	return rep, nil
}
