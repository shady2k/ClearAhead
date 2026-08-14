package worldgen

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/project"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/track"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// Замеры конвейера входа. Ничего не меняют в поведении: их дело — разложить
// стоимость бутстрапа на слагаемые, чтобы стало видно, где она лежит — в
// рельефе, в обходе кандидатов или в SQLite.

func benchStore(tb testing.TB) *worldstore.Store {
	tb.Helper()
	s, err := worldstore.Open(filepath.Join(tb.TempDir(), "world.db"))
	if err != nil {
		tb.Fatalf("база: %v", err)
	}
	tb.Cleanup(func() { s.Close() })
	return s
}

// BenchmarkBootstrap — ХОЛОДНЫЙ СТАРТ: бутстрап (запись региона) и прогрев
// (развёртка чанков) тем же шагом, каким их включает cmd/clearahead. База
// каждый раз свежая, её создание из замера исключено.
//
// Сам по себе бутстрап с 2026-08-13 чанков не порождает (прогрев — кэш, а не
// часть мира), поэтому «весь путь» — это два шага, и мерить их надо вместе:
// оператор платит за оба при каждом холодном старте.
func BenchmarkBootstrap(b *testing.B) {
	m := seedmap.Station(seedmap.WithTerrain())
	var rep Report
	for b.Loop() {
		b.StopTimer()
		s := benchStore(b)
		b.StartTimer()

		if _, seeded, err := Bootstrap(s, m, 1, `{"source":"seedmap","kind":"fixture"}`); err != nil || !seeded {
			b.Fatalf("бутстрап: %v, сделан=%v", err, seeded)
		}
		r, err := Generate(s, m, m.MapID, 1, 1)
		if err != nil {
			b.Fatalf("прогрев: %v", err)
		}
		rep = r

		b.StopTimer()
		s.Close()
		b.StartTimer()
	}
	b.ReportMetric(float64(rep.TotalChunks), "чанков")
}

// BenchmarkPrepare — всё, что делается до первого чанка: валидация карты,
// распространение поз, построение поля.
func BenchmarkPrepare(b *testing.B) {
	m := seedmap.Station(seedmap.WithTerrain())
	b.Run("валидация", func(b *testing.B) {
		for b.Loop() {
			if err := mapfmt.Validate(m); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("позы", func(b *testing.B) {
		for b.Loop() {
			if _, _, err := track.Propagate(m); err != nil {
				b.Fatal(err)
			}
		}
	})
	_, els, err := track.Propagate(m)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("поле", func(b *testing.B) {
		for b.Loop() {
			if _, err := terrain.New(m, els); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// walkField — тот же перебор кандидатов, каким порождает Generate.
//
// Прежде здесь лежала КОПИЯ двойного цикла: разбирать Generate на части ради
// замера значило бы менять код, который меряешь. Теперь обход и правило
// покрытия разделены в самом конвейере (worldgen.walk), и копия стала лишней —
// а вместе с ней ушёл и риск замерить устаревшее правило.
//
// Возвращает: сколько адресов рассмотрено, сколько прошло отбор.
func walkField(f *terrain.Field, region string, emit func(chunk.Address) error) (candidates, selected int, err error) {
	s, ok := selectorFor(f)
	if !ok {
		return 0, 0, nil
	}
	return walk(s, region, s.covers, emit)
}

// selectorFor собирает selector из поля: габарит нужен и обходу, и отсеву.
func selectorFor(f *terrain.Field) (selector, bool) {
	minX, minY, maxX, maxY, ok := f.Bounds()
	if !ok {
		return selector{}, false
	}
	// Правило и домен берутся У ПОЛЯ — там же, где их берёт Generate: охват и
	// существование приезжают картой, и второй их источник в проверках мерил бы
	// не тот мир.
	return selector{field: f, rule: f.Rule(), domain: f.Domain(),
		minX: minX, minY: minY, maxX: maxX, maxY: maxY, hasAxis: true}, true
}

func seedField(tb testing.TB) *terrain.Field {
	tb.Helper()
	m := seedmap.Station(seedmap.WithTerrain())
	_, els, err := track.Propagate(m)
	if err != nil {
		tb.Fatal(err)
	}
	f, err := terrain.New(m, els)
	if err != nil {
		tb.Fatal(err)
	}
	return f
}

// BenchmarkWalkCandidates — только выбор уровня по удалённости, без
// порождения высот и без записи. Это цена «посмотреть и не взять».
func BenchmarkWalkCandidates(b *testing.B) {
	f := seedField(b)
	var candidates, selected int
	for b.Loop() {
		var err error
		candidates, selected, err = walkField(f, "ST_A", nil)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(candidates), "кандидатов")
	b.ReportMetric(float64(selected), "отобрано")
}

// domainAround — домен фикстуры по габариту её оси.
//
// Число выведено из габарита (миграционный выбор, как у st_a.json) и
// выровнено по сетке самого грубого уровня (4096 м): ни одна грубая клетка на
// краю не режется границей пополам. Это выбор ФИКСТУРЫ, а не правило — домен
// есть свойство карты, и длинная ось обязана объявить его сама, иначе её мир
// кончится раньше, чем кончится ось.
func domainAround(minX, minZ, maxX, maxZ, reachM float64) mapfmt.Domain {
	const coarse = 4096.0
	floor := func(v float64) float64 { return math.Floor(v/coarse) * coarse }
	ceil := func(v float64) float64 { return math.Ceil(v/coarse) * coarse }
	return mapfmt.Domain{
		MinX: floor(minX - reachM),
		MinZ: floor(minZ - reachM),
		MaxX: ceil(maxX + reachM),
		MaxZ: ceil(maxZ + reachM),
	}
}

// rightAngleAxis — ось, загибающаяся под прямым углом: два плеча по lengthM.
//
// Нужна затем, что обход кандидатов идёт по ГАБАРИТУ оси, а не вдоль неё.
// Прямая ось даёт узкую полосу и занижает обход; настоящая сеть занимает
// квадрат, и квадрат этот растёт как площадь, а не как длина пути.
func rightAngleAxis(tb testing.TB, lengthM float64) *mapfmt.Map {
	tb.Helper()
	m := seedmap.Corridor(
		seedmap.WithID("UGOL"),
		seedmap.WithTerrain(),
		seedmap.WithoutConstruction(),
		seedmap.Mutate(func(m *mapfmt.Map) {
			m.Geometry.Edges[seedmap.CorridorFirst] = mapfmt.Alignments{
				Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: lengthM}},
			}
			m.Geometry.Edges[seedmap.CorridorSecond] = mapfmt.Alignments{
				Horizontal: []mapfmt.HPrim{
					{Kind: "arc", Radius: 500, Angle: math.Pi / 2},
					{Kind: "straight", Length: lengthM},
				},
			}
		}),
	)
	// Домен по фактическому габариту оси: направление поворота дуги известно
	// только компилятору, и гадать за него фикстура не вправе.
	_, els, err := track.Propagate(m)
	if err != nil {
		tb.Fatalf("угол %.0f м: позы: %v", lengthM, err)
	}
	f, err := terrain.New(m, els)
	if err != nil {
		tb.Fatalf("угол %.0f м: рельеф: %v", lengthM, err)
	}
	minX, minZ, maxX, maxZ, ok := f.Bounds()
	if !ok {
		tb.Fatalf("угол %.0f м: нет оси", lengthM)
	}
	m.Terrain.Domain = domainAround(minX, minZ, maxX, maxZ, 8192)
	if err := mapfmt.Validate(m); err != nil {
		tb.Fatalf("угол %.0f м невалиден: %v", lengthM, err)
	}
	return m
}

// BenchmarkWalkSquareBounds — тот же обход, но по габариту 40 × 40 км.
// Число кандидатов растёт как площадь габарита, и это отдельное слагаемое
// стоимости региона, никак не связанное с числом хранимых чанков.
func BenchmarkWalkSquareBounds(b *testing.B) {
	m := rightAngleAxis(b, 40e3)
	_, els, err := track.Propagate(m)
	if err != nil {
		b.Fatal(err)
	}
	f, err := terrain.New(m, els)
	if err != nil {
		b.Fatal(err)
	}
	var candidates, selected int
	for b.Loop() {
		candidates, selected, err = walkField(f, "UGOL", nil)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(candidates), "кандидатов")
	b.ReportMetric(float64(selected), "отобрано")
}

// BenchmarkWalkWithHeights — обход плюс развёртка отобранных чанков, но БЕЗ
// записи в базу. Разница с полным конвейером и есть цена SQLite.
func BenchmarkWalkWithHeights(b *testing.B) {
	f := seedField(b)
	for b.Loop() {
		_, _, err := walkField(f, "ST_A", func(a chunk.Address) error {
			_, err := f.ChunkHeights(a)
			return err
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// straightCorridor — карта из одного прямого ребра заданной длины. Затравка даёт
// 460 м пути; чтобы говорить о регионе, нужна ось, у которой длина измеряется
// километрами, а строится она кодом.
func straightCorridor(tb testing.TB, lengthM float64) *mapfmt.Map {
	tb.Helper()
	m := seedmap.Line(
		seedmap.WithID("CORR"),
		seedmap.WithTerrain(),
		seedmap.WithoutConstruction(),
		seedmap.Mutate(func(m *mapfmt.Map) {
			m.Geometry.Edges[seedmap.LineEdgeID] = mapfmt.Alignments{
				Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: lengthM}},
			}
			// Прямая ось лежит вдоль x от 0 до lengthM при z = 0: домен
			// объявляется здесь, по габариту оси (миграционный выбор, см.
			// domainAround).
			m.Terrain.Domain = domainAround(0, 0, lengthM, 0, 8192)
		}),
	)
	if err := mapfmt.Validate(m); err != nil {
		tb.Fatalf("коридор %.0f м невалиден: %v", lengthM, err)
	}
	return m
}

// BenchmarkGenerateCorridor — конвейер целиком на оси длиной в километры.
// Здесь и берутся числа для экстраполяции на регион: стоимость километра оси
// вместе с обходом, рельефом и записью.
//
// Двести километров дают около двух минут и полгигабайта во временном каталоге,
// поэтому верхняя длина оставлена двадцатью: линейность видна уже на ней.
func BenchmarkGenerateCorridor(b *testing.B) {
	for _, length := range []float64{2e3, 2e4} {
		m := straightCorridor(b, length)
		b.Run(fmt.Sprintf("%.0fкм", length/1000), func(b *testing.B) {
			var rep Report
			for b.Loop() {
				b.StopTimer()
				s := benchStore(b)
				if err := s.PutRegion(worldstore.Region{ID: m.MapID, Frame: "{}", Epoch: 1, Rule: ruleOf(b, m), Domain: m.Terrain.Domain}); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()

				r, err := Generate(s, m, m.MapID, 1, 1)
				if err != nil {
					b.Fatal(err)
				}
				rep = r

				b.StopTimer()
				s.Close()
				b.StartTimer()
			}
			b.ReportMetric(float64(rep.TotalChunks), "чанков")
			b.ReportMetric(float64(rep.TotalBytes)/1e6, "МБ")
		})
	}
}

// BenchmarkGenerateRegion — Generate на заведённом регионе, база свежая.
func BenchmarkGenerateRegion(b *testing.B) {
	m := seedmap.Station(seedmap.WithTerrain())
	for b.Loop() {
		b.StopTimer()
		s := benchStore(b)
		if err := s.PutRegion(worldstore.Region{ID: "ST_A", Frame: "{}", Epoch: 1, Rule: ruleOf(b, m), Domain: m.Terrain.Domain}); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if _, err := Generate(s, m, "ST_A", 1, 1); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		s.Close()
		b.StartTimer()
	}
}

// BenchmarkRebuildTurnout — стоимость адресной пересборки правки одной
// стрелки на затравке: замыкание, пересборка адресов и запись группами.
//
// Габарит 200 × 40 м (след стрелки с откосами, W1-D) даёт 12 адресов на
// уровне 0..4. Спека §8.1 называет бюджет сквозных 300 мс для подтверждённой
// постройки; результат дороже — это число, а не провал, но назвать его
// обязано измерение.
func BenchmarkRebuildTurnout(b *testing.B) {
	m := seedmap.Station(seedmap.WithTerrain())
	ch := project.Change{
		Kind:   project.SourcePath,
		Extent: project.Extent{MinX: 100, MinZ: -40, MaxX: 300, MaxZ: 0},
	}
	var rep *Result
	for b.Loop() {
		b.StopTimer()
		s := benchStore(b)
		if _, seeded, err := Bootstrap(s, m, 1, `{"source":"seedmap","kind":"fixture"}`); err != nil || !seeded {
			b.Fatalf("бутстрап: %v, сделан=%v", err, seeded)
		}
		if _, err := Generate(s, m, m.MapID, 1, 1); err != nil {
			b.Fatalf("прогрев: %v", err)
		}
		cc := NewCompiler(s, m.MapID, 1, 0)
		b.StartTimer()

		r, err := cc.Rebuild(m, ch)
		if err != nil {
			b.Fatalf("пересборка: %v", err)
		}
		rep = r

		b.StopTimer()
		s.Close()
		b.StartTimer()
	}
	b.ReportMetric(float64(rep.TotalChunks), "адресов")
}
