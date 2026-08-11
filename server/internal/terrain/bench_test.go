package terrain

import (
	"fmt"
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// Замеры стороны данных. Ничего не оптимизируют и ничего не проверяют: их дело
// — числа, по которым принимается решение о масштабе.
//
// Оценка, ради которой они написаны, висит комментарием к полю grid: «6.9 мс на
// чанк при 1263 точках оси; на коридоре 400 тыс. точек и 61 тыс. чанков — около
// 29 часов в один поток на регион». Индекс с тех пор построен, а число не
// пересчитано.

// benchField — то же, что помощник тестов, но принимает testing.TB.
func benchField(tb testing.TB, m *mapfmt.Map) *Field {
	tb.Helper()
	_, els, err := track.Propagate(m)
	if err != nil {
		tb.Fatalf("распространение поз: %v", err)
	}
	f, err := New(m, els)
	if err != nil {
		tb.Fatalf("построение рельефа: %v", err)
	}
	return f
}

// straightAxis — карта из одного прямого ребра заданной длины.
//
// Длинная ось нужна затем, что оценка в комментарии говорит о четырёхстах
// тысячах точек, а станция даёт тысячу с небольшим: без второй точки на кривой
// «стоимость от числа точек» неизвестно, растёт ли она вовсе.
//
// Решётка выброшена (WithoutConstruction): её спаны пришлось бы тянуть за
// длиной, а к рельефу она отношения не имеет.
func straightAxis(tb testing.TB, lengthM float64) *mapfmt.Map {
	tb.Helper()
	m := seedmap.Line(
		seedmap.WithTerrain(),
		seedmap.WithoutConstruction(),
		seedmap.Mutate(func(m *mapfmt.Map) {
			m.Geometry.Edges[seedmap.LineEdgeID] = mapfmt.Alignments{
				Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: lengthM}},
			}
		}),
	)
	if err := mapfmt.Validate(m); err != nil {
		tb.Fatalf("карта длиной %.0f м невалидна: %v", lengthM, err)
	}
	return m
}

// axisLengths — ряд длин прямой оси. Шаг десятикратный, верх выбран так, чтобы
// последняя карта дала ровно тот порядок точек, о котором говорит оценка.
var axisLengths = []float64{2e3, 2e4, 2e5, 2e6}

func lengthName(lengthM float64) string {
	return fmt.Sprintf("%.0fкм", lengthM/1000)
}

// BenchmarkChunkGenerationStation — стоимость одного чанка по уровням на
// затравке. Чанк (0,0) содержит начало оси на любом уровне.
func BenchmarkChunkGenerationStation(b *testing.B) {
	m := seedmap.Station(seedmap.WithTerrain())
	f := benchField(b, m)
	b.Logf("точек оси: %d", len(f.axis))
	for level := 0; level <= chunk.MaxLevel; level++ {
		b.Run(fmt.Sprintf("уровень%d", level), func(b *testing.B) {
			a := chunk.Address{Region: "ST_A", Level: level, CX: 0, CZ: 0}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := f.ChunkHeights(a); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkChunkGenerationFarFromAxis — тот же чанк, но за пределами
// досягаемости земляных работ. Разница с предыдущим показывает, что стоит
// дороже: найти ось или убедиться, что её рядом нет.
func BenchmarkChunkGenerationFarFromAxis(b *testing.B) {
	m := seedmap.Station(seedmap.WithTerrain())
	f := benchField(b, m)
	// 10 чанков нулевого уровня — 2560 м от станции, заведомо вне reach.
	for _, c := range []struct {
		name  string
		cx    int
		level int
	}{
		{"уровень0_вдали", 10, 0},
		{"уровень4_вдали", 10, 4},
	} {
		b.Run(c.name, func(b *testing.B) {
			a := chunk.Address{Region: "ST_A", Level: c.level, CX: c.cx, CZ: 0}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := f.ChunkHeights(a); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkChunkGenerationParallel — тот же чанк на всех ядрах сразу.
//
// Вопрос не праздный: если чанки независимы, срок порождения региона делится на
// число ядер, и это меняет вывод о масштабе. Поле после New не меняется —
// обе карты индекса только читаются, — поэтому гонки здесь взяться неоткуда;
// замер проверяет, что и по времени это так.
func BenchmarkChunkGenerationParallel(b *testing.B) {
	f := benchField(b, seedmap.Station(seedmap.WithTerrain()))
	a := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 0}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := f.ChunkHeights(a); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkChunkGenerationByPointCount — главный замер: зависит ли стоимость
// чанка от размера сети. Чанк берётся в середине оси, чтобы он гарантированно
// лежал в зоне земляных работ на любой длине.
func BenchmarkChunkGenerationByPointCount(b *testing.B) {
	for _, length := range axisLengths {
		m := straightAxis(b, length)
		f := benchField(b, m)
		pointCount := len(f.axis)
		// Середина оси в метрах -> координата чанка нулевого уровня.
		cx := int(length / 2 / chunk.SideM(0))
		b.Run(lengthName(length), func(b *testing.B) {
			a := chunk.Address{Region: "LINE", Level: 0, CX: cx, CZ: 0}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := f.ChunkHeights(a); err != nil {
					b.Fatal(err)
				}
			}
			// После цикла: b.Loop сбрасывает счётчики на первом обороте и
			// стёр бы метрику, объявленную раньше.
			b.ReportMetric(float64(pointCount), "точек_оси")
		})
	}
}

// chunkHeightsByScan — развёртка чанка БЕЗ индекса, честным перебором всех точек
// оси. Это та реализация, на которой снята оценка «6.9 мс на чанк при 1263
// точках оси»; она оставлена в тесте (index_test.go) как эталон правильности, и
// здесь служит эталоном стоимости.
//
// Повторяет WorkedM и HeightCm с точностью до способа поиска ближайшей оси —
// иначе сравнивать было бы нечего.
func (f *Field) chunkHeightsByScan(a chunk.Address) []int16 {
	out := make([]int16, chunk.Samples*chunk.Samples)
	half := f.recipe.Earthworks.FormationHalfWidth
	for j := range chunk.Samples {
		for i := range chunk.Samples {
			x, y := a.SampleM(i, j)
			natural := f.NaturalM(x, y)
			worked := natural
			if d, axisZ, ok := f.nearestAxisByScan(x, y); ok {
				switch {
				case d <= half:
					worked = axisZ
				default:
					drop := (d - half) / f.recipe.Earthworks.SideSlope
					if natural < axisZ {
						worked = math.Max(natural, axisZ-drop)
					} else {
						worked = math.Min(natural, axisZ+drop)
					}
				}
			}
			out[chunk.Index(i, j)] = int16(math.Round((worked - f.recipe.BaseZ) * 100))
		}
	}
	return out
}

// BenchmarkChunkGenerationByScanWithoutIndex — прямая проверка оценки,
// висящей в комментарии к полю grid. Без неё «стало ли лучше» остаётся словом.
func BenchmarkChunkGenerationByScanWithoutIndex(b *testing.B) {
	b.Run("станция", func(b *testing.B) {
		f := benchField(b, seedmap.Station(seedmap.WithTerrain()))
		pointCount := len(f.axis)
		a := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 0}
		for b.Loop() {
			f.chunkHeightsByScan(a)
		}
		b.ReportMetric(float64(pointCount), "точек_оси")
	})
	for _, length := range axisLengths {
		f := benchField(b, straightAxis(b, length))
		pointCount := len(f.axis)
		cx := int(length / 2 / chunk.SideM(0))
		b.Run(lengthName(length), func(b *testing.B) {
			a := chunk.Address{Region: "LINE", Level: 0, CX: cx, CZ: 0}
			for b.Loop() {
				f.chunkHeightsByScan(a)
			}
			b.ReportMetric(float64(pointCount), "точек_оси")
		})
	}
}

// BenchmarkFieldConstruction — terrain.New целиком: выборка оси и оба индекса.
func BenchmarkFieldConstruction(b *testing.B) {
	b.Run("станция", func(b *testing.B) {
		m := seedmap.Station(seedmap.WithTerrain())
		_, els, err := track.Propagate(m)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			if _, err := New(m, els); err != nil {
				b.Fatal(err)
			}
		}
	})
	for _, length := range axisLengths {
		m := straightAxis(b, length)
		_, els, err := track.Propagate(m)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(lengthName(length), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := New(m, els); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAxisSampling и BenchmarkIndexConstruction разделяют стоимость New:
// сколько уходит на геометрию, а сколько на набивку двух карт.
func BenchmarkAxisSampling(b *testing.B) {
	for _, length := range axisLengths {
		m := straightAxis(b, length)
		_, els, err := track.Propagate(m)
		if err != nil {
			b.Fatal(err)
		}
		e := els[seedmap.LineEdgeID]
		b.Run(lengthName(length), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := sampleAxis(e, nil, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkIndexConstruction(b *testing.B) {
	for _, length := range axisLengths {
		f := benchField(b, straightAxis(b, length))
		b.Run(lengthName(length)+"/мелкий", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				newPointGrid(f.reach, f.axis)
			}
		})
		b.Run(lengthName(length)+"/грубый", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				newPointGrid(chunk.Level0RadiusM, f.axis)
			}
		})
	}
}

// BenchmarkSample разбирает стоимость ОДНОГО отсчёта на слагаемые: шум,
// поиск ближайшей оси, и то и другое вместе.
//
// Точки выбраны на станции: одна под осью, одна на откосе, одна заведомо вне
// досягаемости земляных работ.
func BenchmarkSample(b *testing.B) {
	f := benchField(b, seedmap.Station(seedmap.WithTerrain()))
	points := []struct {
		name string
		x, y float64
	}{
		{"под_осью", 60, 0},
		{"на_откосе", 60, 20},
		{"вдали", 60, 3000},
	}
	for _, p := range points {
		b.Run("шум/"+p.name, func(b *testing.B) {
			for b.Loop() {
				_ = f.NaturalM(p.x, p.y)
			}
		})
		b.Run("ближайшая_ось/"+p.name, func(b *testing.B) {
			for b.Loop() {
				_, _, _ = f.nearestAxis(p.x, p.y)
			}
		})
		b.Run("высота/"+p.name, func(b *testing.B) {
			for b.Loop() {
				if _, err := f.HeightCm(p.x, p.y); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDistanceToAxis — запрос грубого индекса, которым конвейер
// выбирает уровень подробности. Спрашивается он РОВНО РАЗ НА ЧАНК-КАНДИДАТ,
// включая те, что не будут порождены, поэтому его цена входит в стоимость
// региона отдельным слагаемым.
func BenchmarkDistanceToAxis(b *testing.B) {
	f := benchField(b, seedmap.Station(seedmap.WithTerrain()))
	for _, p := range []struct {
		name string
		x, y float64
	}{
		{"рядом", 60, 10},
		{"в_кольце", 60, 3000},
		{"мимо", 60, 40000},
	} {
		b.Run(p.name, func(b *testing.B) {
			for b.Loop() {
				_, _ = f.DistanceToAxis(p.x, p.y)
			}
		})
	}
}
