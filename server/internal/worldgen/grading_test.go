package worldgen

import (
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/project"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// gradedCell — клетка уровня 0 на безопасном удалении и от оси затравки, и от
// её реки: покрывает [0, 256) × [-512, -256), ось идёт вдоль y=0, а река
// меандрирует севернее (y ∈ [236, 444]) с охранным радиусом reach = 336 м
// (30 + 46 + 260). Клетка южнее станции: от пути ≥ 256 м (вне коридора
// земляных работ), от реки ≥ 492 м (вне охранной области) — композиция не
// отказывает, и уровень 0 прогрев пишет (384 м от оси ≤ 512 м радиуса).
func gradedCell(cx, cz int, height int16) terrain.GradeCell {
	return terrain.GradeCell{Level: 0, CX: cx, CZ: cz, HeightCm: height}
}

// chunkAtAddr читает строку чанка под версией: отсутствие — ошибка теста.
func chunkAtAddr(t *testing.T, s *worldstore.Store, a chunk.Address, v int64) worldstore.Chunk {
	t.Helper()
	c, ok, err := s.GetChunk(a, v)
	if err != nil || !ok {
		t.Fatalf("чанк %v под версией %d: ok=%v err=%v", a, v, ok, err)
	}
	return c
}

// TestGradingSurvivesReseed — правка высот детерминированно ПЕРЕКОМПИЛИРУЕТСЯ
// из исходника: те же карта и правки дают ту же землю на свежей базе проекций
// (инвариант §3 контракта). Это УСЛОВИЕ сохранности исходника, но не сама
// сохранность: правка здесь переносится в новый вызов переменной, а пережить
// НАСТОЯЩИЙ перезапуск (хранилище исходников -> сервис правок -> конвейер)
// она обязана по sqym.18 — TestGradingSurvivesRestartThroughComposition
// (composition_test.go), приёмочный критерий волны.
func TestGradingSurvivesReseed(t *testing.T) {
	m := newMap(t)
	g := terrain.Grading{Cells: []terrain.GradeCell{gradedCell(0, -2, 500)}}
	graded := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: -2}

	// Первый засев: мир с правкой.
	s1 := newStore(t)
	if _, _, err := Bootstrap(s1, m, 1, `{"source":"seedmap","kind":"fixture"}`); err != nil {
		t.Fatalf("бутстрап: %v", err)
	}
	if _, err := GenerateGraded(s1, m, g, m.MapID, 1, 1); err != nil {
		t.Fatalf("первый засев: %v", err)
	}
	before := chunkAtAddr(t, s1, graded, 1)

	// Пересев: база снесена (новое хранилище), мир засеян заново из тех же
	// исходников — ровно то, что делает -reseed: стереть проекции, засеять
	// из источника.
	s2 := newStore(t)
	if _, _, err := Bootstrap(s2, m, 1, `{"source":"seedmap","kind":"fixture"}`); err != nil {
		t.Fatalf("бутстрап после пересева: %v", err)
	}
	if _, err := GenerateGraded(s2, m, g, m.MapID, 1, 1); err != nil {
		t.Fatalf("пересев: %v", err)
	}
	after := chunkAtAddr(t, s2, graded, 1)
	if !chunkEq(before, after) {
		t.Fatal("пересев изменил землю: чанк правки до и после разошёлся — правка не пережила пересев")
	}

	// Правка видна: плато ровно 500 см, а не природная земля.
	for j := range chunk.Samples - 1 {
		for i := range chunk.Samples - 1 {
			if got := after.Heights[chunk.Index(i, j)]; got != 500 {
				t.Fatalf("отсчёт (%d, %d) после пересева: %d см, ожидалось 500 — правка не применилась", i, j, got)
			}
		}
	}

	// Контроль: мир БЕЗ правки даёт на этой клетке природную землю — плато
	// отличается от натуры, а не от пустоты.
	s0 := newStore(t)
	if _, _, err := Bootstrap(s0, m, 1, `{"source":"seedmap","kind":"fixture"}`); err != nil {
		t.Fatalf("бутстрап эталона: %v", err)
	}
	if _, err := Generate(s0, m, m.MapID, 1, 1); err != nil {
		t.Fatalf("эталон: %v", err)
	}
	if chunkEq(before, chunkAtAddr(t, s0, graded, 1)) {
		t.Fatal("правка не изменила землю: чанк с правкой равен природному")
	}
}

// TestGradingLazyKnowsPatch — порождение по требованию обязано знать правки:
// клетка, засеянная правкой, но спрошенная лениво (камера подлетела ближе,
// прогрев её не клал), обязана посчитаться С правкой, а не природной землёй.
func TestGradingLazyKnowsPatch(t *testing.T) {
	m := newMap(t)
	g := terrain.Grading{Cells: []terrain.GradeCell{gradedCell(0, -2, 500)}}
	graded := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: -2}

	s := newStore(t)
	if _, _, err := Bootstrap(s, m, 1, `{"source":"seedmap","kind":"fixture"}`); err != nil {
		t.Fatalf("бутстрап: %v", err)
	}
	// Прогрев БЕЗ правки: клетка (0, -2) в базе остаётся природной.
	if _, err := Generate(s, m, m.MapID, 1, 1); err != nil {
		t.Fatalf("прогрев: %v", err)
	}
	lazy, err := NewLazyGraded(s, m, g, m.MapID, 1)
	if err != nil {
		t.Fatalf("порождение: %v", err)
	}
	// Принудительная пересборка по требованию: ответ обязан нести плато.
	ch, ok, err := lazy.MakeChunk(graded, 1)
	if err != nil || !ok {
		t.Fatalf("MakeChunk: ok=%v err=%v", ok, err)
	}
	for j := range chunk.Samples - 1 {
		for i := range chunk.Samples - 1 {
			if got := ch.Heights[chunk.Index(i, j)]; got != 500 {
				t.Fatalf("отсчёт (%d, %d) ленивого порождения: %d см, ожидалось 500 — правка не дошла до Lazy", i, j, got)
			}
		}
	}
}

// TestGradingChangeRebuildsClosureAllLevels — правка высот попадает в
// замыкание инвалидизации (project.Closure, SourceGrading) и тянет
// пересборку затронутых чанков НА ВСЕХ уровнях подробности: поверхность —
// проекция levelsEvery, и клетка правки пересобирается от уровня 0 до
// последнего. Пересобранные строки байт в байт равны полной компиляции мира
// с правкой (детерминизм §3 контракта).
func TestGradingChangeRebuildsClosureAllLevels(t *testing.T) {
	_, s := rebuildFixture(t)
	m := newMap(t)
	g := terrain.Grading{Cells: []terrain.GradeCell{gradedCell(0, -2, 500)}}
	ccG := NewCompilerGraded(s, "ST_A", 1, 0, g)

	graded := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: -2}
	minX, minZ, maxX, maxZ := g.Cells[0].Bounds()
	ch := project.Change{Kind: project.SourceGrading,
		Extent: project.Extent{MinX: minX, MinZ: minZ, MaxX: maxX, MaxZ: maxZ}}

	res, err := ccG.Rebuild(m, ch)
	if err != nil {
		t.Fatalf("пересборка: %v", err)
	}
	if res.TotalChunks == 0 {
		t.Fatal("замыкание правки высот пусто — пересобрано нечего")
	}

	// Все уровни: у поверхности levelsEvery, и клетка правки задевает каждый
	// уровень от нулевого до последнего.
	reg, err := regionOf(m)
	if err != nil {
		t.Fatalf("регион: %v", err)
	}
	want := closureAddresses(t, reg, ch)
	for level := 0; level <= reg.Rule.MaxLevel; level++ {
		found := false
		for a := range want {
			if a.Level == level {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("замыкание правки высот не задело уровень %d", level)
		}
	}
	if !want[graded] {
		t.Fatal("клетка правки отсутствует в замыкании уровня 0")
	}

	// Пересобранная земля: плато правки под версией публикации.
	rebV := publishedVersion(t, s)
	c := chunkAtAddr(t, s, graded, rebV)
	for j := range chunk.Samples - 1 {
		for i := range chunk.Samples - 1 {
			if got := c.Heights[chunk.Index(i, j)]; got != 500 {
				t.Fatalf("отсчёт (%d, %d) после пересборки: %d см, ожидалось 500", i, j, got)
			}
		}
	}

	// Детерминизм: строка замыкания равна полной компиляции мира с правкой.
	full := compileByAddress(t, ccG, m)
	for a := range want {
		if !chunkEq(full[a], chunkAtAddr(t, s, a, rebV)) {
			t.Fatalf("чанк %v пересобран не как полная компиляция — детерминизм §3 нарушен", a)
		}
	}
}

// TestBuildingTouchingFourChunksInvalidatesAll — постройка, чей земляной след
// (пятно плюс откос) пересекает границы четырёх чанков уровня 0, обязана
// попасть в замыкание инвалидизации ВСЕХ четырёх, а не одного «домашнего» по
// точке привязки (sqym.15). Постройка шириной 16–30 м покрывает от 16 до 64
// ячеек уровня 0 (region-objects-design §4.4): домашняя клетка — про
// РАЗМЕЩЕНИЕ в ресурсе выдачи, а не про инвалидизацию; тождество постройки —
// UUIDv7, и от габарита оно не зависит.
//
// Габарит изменения считает terrain.BuildingEarthExtent (пятно + откос), и
// тест проверяет именно его: вернись кто к «домашней клетке» точки привязки —
// замыкание дало бы один чанк, и пересборка оставила бы три соседних на
// старой земле (шовный инвариант рвётся на границе).
func TestBuildingTouchingFourChunksInvalidatesAll(t *testing.T) {
	m := newMap(t)
	// Пятно 30×30 м с центром в (250, -250): [235, 265] × [-265, -235],
	// пересекает границы x=256 и z=-256 — четыре чанка уровня 0 вокруг угла.
	// Откос считаем по размаху шума затравки: total = 18 + 3 = 21 м, откос
	// уходит на maxDrop·SideSlope = 2·21·1.5 = 63 м (максимальный перепад
	// двух точек природной поверхности — 2·total).
	b := mapfmt.Building{ID: "bld-corner", X: 250, Y: -250, Heading: 0, Width: 30, Depth: 30, Height: 8}
	total := 0.0
	for _, o := range m.Terrain.Octaves {
		total += o.AmplitudeM
	}
	minX, minZ, maxX, maxZ := terrain.BuildingEarthExtent(b, m.Terrain.Earthworks.SideSlope, 2*total)
	ch := project.Change{
		Kind:   project.SourceStructure,
		Extent: project.Extent{MinX: minX, MinZ: minZ, MaxX: maxX, MaxZ: maxZ},
	}
	reg, err := regionOf(m)
	if err != nil {
		t.Fatalf("регион: %v", err)
	}
	res, err := reg.Closure(ch)
	if err != nil {
		t.Fatalf("замыкание: %v", err)
	}
	surf := entryOfProject(t, res, project.GroupSurface, project.Surface)
	if surf == nil {
		t.Fatal("поверхность отсутствует в замыкании постройки")
	}
	for _, want := range []project.Address{
		{Level: 0, CX: 0, CZ: -1}, {Level: 0, CX: 1, CZ: -1},
		{Level: 0, CX: 0, CZ: -2}, {Level: 0, CX: 1, CZ: -2},
	} {
		if !containsProjectAddr(surf.Addresses, want) {
			t.Errorf("постройка не попала в замыкание чанка %+v — габарит изменения обязан накрыть все четыре", want)
		}
	}
}

func entryOfProject(t *testing.T, res *project.Result, g project.Group, p project.Projection) *project.Entry {
	t.Helper()
	for i := range res.Groups {
		if res.Groups[i].Group != g {
			continue
		}
		for j := range res.Groups[i].Entries {
			if res.Groups[i].Entries[j].Projection == p {
				return &res.Groups[i].Entries[j]
			}
		}
	}
	return nil
}

func containsProjectAddr(addrs []project.Address, a project.Address) bool {
	for _, x := range addrs {
		if x == a {
			return true
		}
	}
	return false
}
