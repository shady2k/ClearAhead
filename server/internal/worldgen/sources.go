// Исходники партии в конвейере мира: правки высот и растительность.
//
// Обе группы — ИСХОДНИКИ по контракту (sources-compilers-projections §1):
// журналируются, переживают пересев, и мир пересобирается из них. Пустой
// Sources — мир без правок и рубок, байт в байт прежний: пустое множество
// правок даёт то же поле, что его отсутствие (инвариант детерминизма §3), и
// та же дисциплина держит растительность — с тем отличием, что НЕПУСТАЯ
// растительность на карте без покрова — отказ (терять журналируемую рубку
// молча запрещено, vegetation.Project).
package worldgen

import (
	"math"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/vegetation"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// Sources — исходники партии, которые компиляторы мира читают рядом с картой:
// правки высот (прямой терраморфинг) и источники растительности (вырубка,
// посадка). Живут в sourcestore и приходят в конвейер одной связкой, чтобы
// прогрев, порождение по требованию и пересборка не разошлись по составу.
type Sources struct {
	Grading    terrain.Grading
	Vegetation vegetation.Sources
}

// GenerateSources — Generate над миром с исходниками партии: правки высот и
// вырубка входят в поле и в лес тем же путём входа, что и карта. Источники —
// исходники, и прогрев над ними даёт ровно ту землю и тот лес, которые клиент
// получит после пересева: чанк — проекция, её можно сносить и считать заново,
// исходник от этого не теряется (sqym.18).
func GenerateSources(s *worldstore.Store, m *mapfmt.Map, src Sources, region string, revision, worldVersion int64) (Report, error) {
	rep := Report{Region: region, ByLevel: map[int]int{}}

	sel, err := prepare(s, m, region, src.Grading)
	if err != nil {
		return rep, err
	}
	baseZmm := int64(math.Round(sel.field.BaseZ() * 1000))

	_, _, err = walk(sel, region, sel.covers, func(a chunk.Address) error {
		ch, err := chunkAt(sel.field, baseZmm, a, revision, worldVersion, src.Vegetation)
		if err != nil {
			return err
		}
		if err := s.PutChunk(ch); err != nil {
			return err
		}
		rep.ByLevel[a.Level]++
		rep.TotalChunks++
		rep.TotalBytes += chunk.HeightsBytes + len(ch.Cover) + len(ch.Forest)
		return nil
	})
	if err != nil {
		return rep, err
	}
	return rep, nil
}

// projectForest — лес чанка как ПРОЕКЦИЯ: лес рецепта минус вырубка плюс
// посадка (vegetation.Project). Пустые исходники — быстрый путь и прежнее
// поведение: лес ровно рецепта, без единого лишнего вызова; непустые проходят
// отбор релевантности (источники адресуют чанк или область, а не весь мир).
func projectForest(field *terrain.Field, a chunk.Address, cover []byte, veg vegetation.Sources) ([]byte, error) {
	if len(veg.Cuts) == 0 && len(veg.Planted) == 0 && len(veg.Clearings) == 0 && len(veg.CutMasks) == 0 {
		return field.ChunkForest(a, cover), nil
	}
	return vegetation.Project(a, field.ChunkForest(a, cover), cover, filterVegetation(veg, a))
}

// filterVegetation отбирает исходники, релевантные ОДНОМУ чанку уровня 0
// (vegetation.Sources — множество для чанка, отбор — забота вызывающего).
// Чанко-адресные исходники (Cut, Planted, CutMask) отбираются по адресу;
// областные (Clearing) — по пересечению прямоугольника с сеткой центров
// ячеек чанка. Чужой исходник не должен доехать до компилятора: Project
// отказывает на чужой ячейке, и мир не публиковался бы из-за рубки соседнего
// чанка.
func filterVegetation(src vegetation.Sources, a chunk.Address) vegetation.Sources {
	out := vegetation.Sources{}
	if a.Level != chunk.ForestLevel {
		return out
	}
	for _, c := range src.Cuts {
		if c.CX == a.CX && c.CZ == a.CZ {
			out.Cuts = append(out.Cuts, c)
		}
	}
	for _, p := range src.Planted {
		if p.CX == a.CX && p.CZ == a.CZ {
			out.Planted = append(out.Planted, p)
		}
	}
	for _, m := range src.CutMasks {
		if m.CX == a.CX && m.CZ == a.CZ {
			out.CutMasks = append(out.CutMasks, m)
		}
	}
	for _, cl := range src.Clearings {
		if clearingTouchesChunk(cl, a) {
			out.Clearings = append(out.Clearings, cl)
		}
	}
	return out
}

// clearingTouchesChunk — пересекает ли прямоугольник вырубки сетку центров
// ячеек чанка: ячейка вырублена, если её ЦЕНТР лежит в прямоугольнике
// (vegetation.Clearing), и релевантность области — пересечение с решёткой
// центров, а не с телом чанка. Центр ячейки (i, j) уровня 0 — (cx·256 + 4i + 2,
// cz·256 + 4j + 2), решётка центров чанка — [cx·256+2, cx·256+254]².
func clearingTouchesChunk(c vegetation.Clearing, a chunk.Address) bool {
	minX := float64(a.CX)*chunk.SideM0 + chunk.StepM0/2
	minZ := float64(a.CZ)*chunk.SideM0 + chunk.StepM0/2
	maxX := minX + chunk.SideM0 - chunk.StepM0
	maxZ := minZ + chunk.SideM0 - chunk.StepM0
	return c.MinX <= maxX && c.MaxX >= minX && c.MinZ <= maxZ && c.MaxZ >= minZ
}
