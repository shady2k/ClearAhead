package project

import (
	"math"
	"reflect"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
)

// testRegion — регион в духе затравки ST_A: охват 512·2⁴ = 8192 м, пять
// уровней, без рек. Вырубка вокруг постройки — 30 м (terrain.go:139).
func testRegion() Region {
	return Region{
		Rule:           chunk.Rule{Level0RadiusM: 512, MaxLevel: 4},
		BuildingClearM: 30.0,
	}
}

// testRiver — река вдоль оси z = 0 от x = 0 до x = 1000 с reach = 100:
// 10 (гладь) + 5 (берег) + 85 (долина).
func testRiver() River {
	return River{
		ID:         "р-1",
		Axis:       []Point{{X: 0, Z: 0}, {X: 1000, Z: 0}},
		HalfWidthM: 10,
		BankM:      5,
		ValleyM:    85,
	}
}

func mustClosure(t *testing.T, r Region, ch Change) *Result {
	t.Helper()
	res, err := r.Closure(ch)
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	return res
}

func groupOf(t *testing.T, res *Result, g Group) *GroupPlan {
	t.Helper()
	for i := range res.Groups {
		if res.Groups[i].Group == g {
			return &res.Groups[i]
		}
	}
	return nil
}

func entryOf(t *testing.T, plan *GroupPlan, p Projection) *Entry {
	t.Helper()
	if plan == nil {
		return nil
	}
	for i := range plan.Entries {
		if plan.Entries[i].Projection == p {
			return &plan.Entries[i]
		}
	}
	return nil
}

func containsAddr(addrs []Address, a Address) bool {
	for _, x := range addrs {
		if x == a {
			return true
		}
	}
	return false
}

func levelSet(addrs []Address) map[int]bool {
	out := make(map[int]bool)
	for _, a := range addrs {
		out[a.Level] = true
	}
	return out
}

// TestClosureUnknownSourceRefused — неизвестный вид исходника даёт ОТКАЗ, а не
// пустое замыкание: пустое замыкание означало бы «ничего пересобирать не
// надо» и молча оставило бы старую землю.
func TestClosureUnknownSourceRefused(t *testing.T) {
	_, err := testRegion().Closure(Change{
		Kind:   SourceKind(99),
		Extent: Extent{MinX: 0, MinZ: 0, MaxX: 100, MaxZ: 100},
	})
	if err == nil {
		t.Fatal("неизвестный вид исходника принят без отказа")
	}
	want := "неизвестный вид исходника"
	if !containsStr(err.Error(), want) {
		t.Errorf("отказ %q не называет причину %q", err, want)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestClosureBadExtentRefused — некорректный габарит — отказ, а не подстановка.
func TestClosureBadExtentRefused(t *testing.T) {
	r := testRegion()
	cases := []Extent{
		{MinX: 100, MinZ: 0, MaxX: 10, MaxZ: 100},         // min > max
		{MinX: 0, MinZ: 100, MaxX: 100, MaxZ: 10},         // min > max по z
		{MinX: math.NaN(), MinZ: 0, MaxX: 100, MaxZ: 100}, // неконечное
		{MinX: 0, MinZ: 0, MaxX: math.Inf(1), MaxZ: 100},  // бесконечное
	}
	for _, e := range cases {
		_, err := r.Closure(Change{Kind: SourcePath, Extent: e})
		if err == nil {
			t.Errorf("габарит %+v принят — ожидался отказ", e)
		}
	}
}

// TestClosureInvalidRegionRefused — неполный регион — отказ: нулевое правило
// похоже на забытую строку, а не на «мира нет».
func TestClosureInvalidRegionRefused(t *testing.T) {
	ch := Change{Kind: SourcePath, Extent: Extent{MinX: 0, MinZ: 0, MaxX: 100, MaxZ: 100}}
	if _, err := (Region{}).Closure(ch); err == nil {
		t.Error("пустой регион принят")
	}
	r := testRegion()
	r.BuildingClearM = -1
	if _, err := r.Closure(ch); err == nil {
		t.Error("отрицательная вырубка принята")
	}
	r = testRegion()
	r.Rivers = []River{{ID: "битая", Axis: []Point{{X: 0, Z: 0}}}}
	if _, err := r.Closure(ch); err == nil {
		t.Error("река из одной точки принята")
	}
}

// TestClosureTransitiveClearingToForest — замыкание транзитивно: правка
// вырубки обязана дотянуться до леса через покров, а не остановиться на нём.
func TestClosureTransitiveClearingToForest(t *testing.T) {
	res := mustClosure(t, testRegion(), Change{
		Kind:   SourceClearing,
		Extent: Extent{MinX: 300, MinZ: 200, MaxX: 500, MaxZ: 240},
	})
	veg := groupOf(t, res, GroupVegetation)
	if veg == nil {
		t.Fatal("группа растительности не появилась — замыкание не дотянулось до леса")
	}
	cover := entryOf(t, veg, Cover)
	forest := entryOf(t, veg, Vegetation)
	if cover == nil || len(cover.Addresses) == 0 {
		t.Error("покров в замыкании отсутствует или пуст")
	}
	if forest == nil || len(forest.Addresses) == 0 {
		t.Error("лес в замыкании отсутствует — замыкание остановилось на покрове")
	}
	if !levelSet(forest.Addresses)[0] {
		t.Error("лес не покрывает уровень 0 (chunk.ForestLevel)")
	}
}

// TestClosurePathReachesForest — путь достигает лес ТРАНЗИТИВНО через покров:
// полоса отчуждения меняет класс ячейки, лес читает класс (terrain.go:1039,
// 1119–1142).
func TestClosurePathReachesForest(t *testing.T) {
	res := mustClosure(t, testRegion(), Change{
		Kind:   SourcePath,
		Extent: Extent{MinX: 300, MinZ: 200, MaxX: 500, MaxZ: 240},
	})
	veg := groupOf(t, res, GroupVegetation)
	if veg == nil {
		t.Fatal("правка пути не дотянулась до растительности")
	}
	for _, want := range []Projection{Cover, Vegetation} {
		if e := entryOf(t, veg, want); e == nil || len(e.Addresses) == 0 {
			t.Errorf("проекция %d отсутствует в замыкании пути", int(want))
		}
	}
}

// TestClosureAllLevels — замыкание на ВСЕХ уровнях, а не только на нулевом:
// патчи строятся для каждого уровня из одной проектной поверхности (§5.3),
// иначе выемка исчезает при отдалении камеры. Лес — только уровень 0.
func TestClosureAllLevels(t *testing.T) {
	res := mustClosure(t, testRegion(), Change{
		Kind:   SourcePath,
		Extent: Extent{MinX: 300, MinZ: 200, MaxX: 500, MaxZ: 240},
	})
	surf := entryOf(t, groupOf(t, res, GroupSurface), Surface)
	if surf == nil {
		t.Fatal("поверхность отсутствует")
	}
	want := map[int]bool{0: true, 1: true, 2: true, 3: true, 4: true}
	if got := levelSet(surf.Addresses); !reflect.DeepEqual(got, want) {
		t.Errorf("уровни поверхности %v, ожидались %v", got, want)
	}
	cover := entryOf(t, groupOf(t, res, GroupVegetation), Cover)
	if got := levelSet(cover.Addresses); !reflect.DeepEqual(got, want) {
		t.Errorf("уровни покрова %v, ожидались %v", got, want)
	}
	geo := entryOf(t, groupOf(t, res, GroupGeometry), Geometry)
	if got := levelSet(geo.Addresses); !reflect.DeepEqual(got, want) {
		t.Errorf("уровни геометрии %v, ожидались %v", got, want)
	}
	forest := entryOf(t, groupOf(t, res, GroupVegetation), Vegetation)
	if got := levelSet(forest.Addresses); !reflect.DeepEqual(got, map[int]bool{0: true}) {
		t.Errorf("уровни леса %v, ожидался только 0 (chunk.ForestLevel)", got)
	}
}

// TestClosureSeamNeighborsSameGroup — габарит, коснувшийся границы чанка,
// захватывает соседей по шву, и все патчи поверхности лежат в ОДНОЙ группе:
// граничный отсчёт принадлежит обоим соседям и обязан публиковаться с ними
// одной версией, иначе вернутся трещины (§5.1).
func TestClosureSeamNeighborsSameGroup(t *testing.T) {
	// Габарит ровно [0, 256]: касается границы мира в нуле и границы между
	// чанками 0 и 1 в 256. Замкнутые клетки уровня 0: cx, cz ∈ {-1, 0, 1}.
	res := mustClosure(t, testRegion(), Change{
		Kind:   SourcePath,
		Extent: Extent{MinX: 0, MinZ: 0, MaxX: 256, MaxZ: 256},
	})
	surf := entryOf(t, groupOf(t, res, GroupSurface), Surface)
	if surf == nil {
		t.Fatal("поверхность отсутствует")
	}
	for _, a := range []Address{
		{Level: 0, CX: 0, CZ: 0},
		{Level: 0, CX: -1, CZ: 0}, // сосед слева по шву
		{Level: 0, CX: 1, CZ: 0},  // сосед справа по шву
	} {
		if !containsAddr(surf.Addresses, a) {
			t.Errorf("шовный сосед %+v отсутствует в замыкании поверхности", a)
		}
	}
	// Соседи по шву обязаны публиковаться с основным чанком ОДНОЙ версией —
	// это и есть группа: вся поверхность замыкания в одной GroupSurface.
	if n := len(groupOf(t, res, GroupSurface).Entries); n != 1 {
		t.Errorf("поверхность разложена на %d записей, ожидалась одна группа", n)
	}
}

// TestClosureSeamOnlyOnClosedCells — покров шва не знает: ячейка — площадь без
// общих рядов (CoverCells = Samples − 1), и габарит, коснувшийся границы,
// соседа не захватывает. Дискриминирующий тест замкнутых клеток.
func TestClosureSeamOnlyOnClosedCells(t *testing.T) {
	res := mustClosure(t, testRegion(), Change{
		Kind:   SourceClearing,
		Extent: Extent{MinX: 0, MinZ: 0, MaxX: 256, MaxZ: 256},
	})
	cover := entryOf(t, groupOf(t, res, GroupVegetation), Cover)
	if cover == nil {
		t.Fatal("покров отсутствует")
	}
	// Покров живёт на каждом уровне, но ни на одном не захватывает шовных
	// соседей: у ячейки нет общих рядов, и габарит [0, 256] целиком лежит в
	// чанке (0, 0) своего уровня.
	if len(cover.Addresses) != 5 {
		t.Fatalf("покров: адресов %d, ожидалось 5 (по одному чанку на уровень)", len(cover.Addresses))
	}
	for _, a := range cover.Addresses {
		if a.CX != 0 || a.CZ != 0 {
			t.Errorf("покров захватил шовного соседа: %+v", a)
		}
	}
}

// TestClosureNoSeamInsideChunk — габарит целиком внутри чанка соседей не
// порождает ни на одном уровне.
func TestClosureNoSeamInsideChunk(t *testing.T) {
	res := mustClosure(t, testRegion(), Change{
		Kind:   SourcePath,
		Extent: Extent{MinX: 100, MinZ: 100, MaxX: 156, MaxZ: 156},
	})
	surf := entryOf(t, groupOf(t, res, GroupSurface), Surface)
	for _, a := range surf.Addresses {
		if a.Level == 0 && (a.CX != 0 || a.CZ != 0) {
			t.Errorf("неожиданный адрес уровня 0 %+v", a)
		}
	}
}

// TestClosureFiniteTurnout — замыкание конечно и не зовёт полный регион.
//
// Замер на затравочной карте (правило ST_A: r0 = 512 м, уровней 5): правка
// одной стрелки с земляным следом 200 × 40 м даёт адреса на каждый уровень по
// одному чанку — граница §8.1 «до четырёх чанков на уровень и до двадцати на
// пять уровней» держится с запасом. Полный пересчёт региона (76 630 чанков,
// ~7 минут) в этот порядок не входит.
func TestClosureFiniteTurnout(t *testing.T) {
	res := mustClosure(t, testRegion(), Change{
		Kind:   SourcePath,
		Extent: Extent{MinX: 300, MinZ: 200, MaxX: 500, MaxZ: 240},
	})
	addrs := res.ChunkAddresses()
	perLevel := make(map[int]int)
	for _, a := range addrs {
		perLevel[a.Level]++
	}
	t.Logf("правка одной стрелки: адресов чанков %d, по уровням %v", len(addrs), perLevel)
	if len(addrs) == 0 {
		t.Fatal("замыкание пусто")
	}
	if len(addrs) > 20 {
		t.Errorf("замыкание одной стрелки даёт %d адресов — порядок больше двадцатки, дефект", len(addrs))
	}
	for l, n := range perLevel {
		if n > 4 {
			t.Errorf("уровень %d даёт %d чанков — больше четырёх, дефект", l, n)
		}
	}
}

// TestClosureNetworkWorldRoot — сеть компилируется целиком: у правки пути
// ровно один адрес — корень мира.
func TestClosureNetworkWorldRoot(t *testing.T) {
	res := mustClosure(t, testRegion(), Change{
		Kind:   SourcePath,
		Extent: Extent{MinX: 300, MinZ: 200, MaxX: 500, MaxZ: 240},
	})
	net := entryOf(t, groupOf(t, res, GroupNetwork), Network)
	if net == nil {
		t.Fatal("сеть отсутствует в замыкании пути")
	}
	if !reflect.DeepEqual(net.Addresses, []Address{WorldRoot}) {
		t.Errorf("адреса сети %+v, ожидался корень мира", net.Addresses)
	}
}

// TestClosureNoWaterFromPath — вода не входит в замыкание земляного эффекта:
// пересечение охранной области реки есть ОТКАЗ, а не пересчёт воды (§5.4).
// Далёкая от реки правка пути воды не порождает вовсе.
func TestClosureNoWaterFromPath(t *testing.T) {
	r := testRegion()
	r.Rivers = []River{testRiver()}
	res := mustClosure(t, r, Change{
		Kind:   SourcePath,
		Extent: Extent{MinX: 1200, MinZ: -50, MaxX: 1300, MaxZ: 50}, // в 200 м от русла
	})
	if g := groupOf(t, res, GroupWater); g != nil {
		t.Error("правка пути породила группу воды")
	}
}

// TestClosureRiverRefusal — земляной эффект, чей габарит пересекает охранную
// область реки, отвергается с причиной: называется и река, и расстояние, и
// радиус.
func TestClosureRiverRefusal(t *testing.T) {
	r := testRegion()
	r.Rivers = []River{testRiver()}
	cases := []Extent{
		{MinX: 950, MinZ: -50, MaxX: 1050, MaxZ: 50},  // пересекает русло
		{MinX: 1090, MinZ: -50, MaxX: 1100, MaxZ: 50}, // в 90 м — внутри reach 100
		{MinX: 100, MinZ: -50, MaxX: 900, MaxZ: 50},   // русло внутри габарита
	}
	for _, e := range cases {
		_, err := r.Closure(Change{Kind: SourcePath, Extent: e})
		if err == nil {
			t.Errorf("габарит %+v пересекает охранную область — отказа нет", e)
			continue
		}
		for _, want := range []string{"охранную область", "р-1"} {
			if !containsStr(err.Error(), want) {
				t.Errorf("отказ %q не называет %q", err, want)
			}
		}
	}
	// За пределами reach отказ не наступает.
	for _, e := range []Extent{
		{MinX: 1101, MinZ: -50, MaxX: 1200, MaxZ: 50}, // в 101 м — снаружи
	} {
		if _, err := r.Closure(Change{Kind: SourcePath, Extent: e}); err != nil {
			t.Errorf("габарит %+v вне охранной области — ложный отказ %v", e, err)
		}
	}
}

// TestClosureRiverRefusalAllKinds — отказ касается ЛЮБОГО земляного эффекта:
// терраморфинг, здание и тип решётки у реки отвергаются так же, как путь.
func TestClosureRiverRefusalAllKinds(t *testing.T) {
	r := testRegion()
	r.Rivers = []River{testRiver()}
	e := Extent{MinX: 950, MinZ: -50, MaxX: 1050, MaxZ: 50}
	for _, k := range []SourceKind{SourceGrading, SourceStructure, SourceTrackType} {
		if _, err := r.Closure(Change{Kind: k, Extent: e}); err == nil {
			t.Errorf("вид %d пересекает охранную область — отказа нет", int(k))
		}
	}
	// Вырубка земли не трогает — у реки она законна.
	if _, err := r.Closure(Change{Kind: SourceClearing, Extent: e}); err != nil {
		t.Errorf("вырубка у реки отвергнута: %v", err)
	}
	// Правка самой реки — не пересечение: у реки своя правка.
	if _, err := r.Closure(Change{Kind: SourceRiver, Extent: e}); err != nil {
		t.Errorf("правка реки отвергнута её же охранной областью: %v", err)
	}
}

// TestClosureRiverChangeInvalidates — правка реки инвалидирует природную и
// проектную поверхность, воду и покров, а по рёбрам — лес и геометрию
// (§1.3 контракта).
func TestClosureRiverChangeInvalidates(t *testing.T) {
	r := testRegion()
	r.Rivers = []River{testRiver()}
	res := mustClosure(t, r, Change{
		Kind:   SourceRiver,
		Extent: Extent{MinX: 200, MinZ: -50, MaxX: 300, MaxZ: 50},
	})
	if g := groupOf(t, res, GroupWater); g == nil {
		t.Fatal("правка реки не породила воду")
	}
	for _, want := range []Projection{Surface, Water, Cover} {
		if e := entryOf(t, groupOf(t, res, groupOfProjection(want)), want); e == nil || len(e.Addresses) == 0 {
			t.Errorf("проекция %d отсутствует в замыкании реки", int(want))
		}
	}
	// Транзитивность: поверхность → геометрия, покров → лес.
	for _, want := range []Projection{Geometry, Vegetation} {
		if e := entryOf(t, groupOf(t, res, groupOfProjection(want)), want); e == nil || len(e.Addresses) == 0 {
			t.Errorf("транзитивная проекция %d отсутствует в замыкании реки", int(want))
		}
	}
}

func groupOfProjection(p Projection) Group {
	if d := declaration(p); d != nil {
		return d.Group
	}
	return Group(-1)
}

// TestClosureGradingNeverTouchesCover — прямой терраморфинг покров НЕ
// инвалидирует, БЕЗУСЛОВНО (решение координатора, sqym.13): «свежая земля
// гола» флагом рецепта не заводится — форма без потребителя, потому что
// покров строится из природных масок, реки, оси и вырубки и WorkedM не зовёт
// (terrain.go:987–1066). Флага больше нет вовсе, и тест проверяет правило, а
// не его переключатель: терраморфинг трогает землю и геометрию, но не покров
// и не лес. Понадобится голая земля — она станет отдельным земляным эффектом
// со своим исходником, а не флагом рецепта.
func TestClosureGradingNeverTouchesCover(t *testing.T) {
	ch := Change{Kind: SourceGrading, Extent: Extent{MinX: 300, MinZ: 200, MaxX: 500, MaxZ: 240}}
	res := mustClosure(t, testRegion(), ch)
	if g := groupOf(t, res, GroupVegetation); g != nil {
		t.Error("терраморфинг тронул покров — правило безусловно, флага нет")
	}
	if g := groupOf(t, res, GroupSurface); g == nil {
		t.Error("терраморфинг не тронул поверхность")
	}
}

// TestCoverDeclarationExcludesGrading — объявление покрова не называет прямой
// терраморфинг своим входом: объявление и Closure — один источник правила
// (sqym.13), и расхождение между ними — баг одного из двух.
func TestCoverDeclarationExcludesGrading(t *testing.T) {
	d := declaration(Cover)
	if d == nil {
		t.Fatal("объявление покрова отсутствует")
	}
	for _, in := range d.Inputs {
		if in == SourceGrading {
			t.Errorf("покров объявляет входом прямой терраморфинг — правило безусловно (sqym.13)")
		}
	}
}

// TestClosureStructureClearingBeyondFootprint — вырубка вокруг постройки живёт
// ЗА габаритом пятна (terrain.go:317): лес и покров пересобираются в круге
// вырубки, поверхность — только в пятне.
func TestClosureStructureClearingBeyondFootprint(t *testing.T) {
	res := mustClosure(t, testRegion(), Change{
		Kind:   SourceStructure,
		Extent: Extent{MinX: 240, MinZ: 240, MaxX: 250, MaxZ: 250},
	})
	surf := entryOf(t, groupOf(t, res, GroupSurface), Surface)
	if surf == nil {
		t.Fatal("поверхность отсутствует")
	}
	if containsAddr(surf.Addresses, Address{Level: 0, CX: 1, CZ: 0}) {
		t.Error("поверхность захватила соседний чанк — пятно внутри чанка 0")
	}
	forest := entryOf(t, groupOf(t, res, GroupVegetation), Vegetation)
	if forest == nil {
		t.Fatal("лес отсутствует")
	}
	// Круг вырубки (радиус 30 м за пятном) пересекает границу чанков в 256:
	// [210, 280] по x — лес пересобирается в чанках 0 и 1.
	if !containsAddr(forest.Addresses, Address{Level: 0, CX: 1, CZ: 0}) {
		t.Error("вырубка за габаритом не дотянулась до соседнего чанка")
	}
}

// TestClosureDeterministic — одинаковый вход даёт побайтово одинаковое
// замыкание: журнал и сравнение версий полагаются на детерминизм.
func TestClosureDeterministic(t *testing.T) {
	ch := Change{Kind: SourcePath, Extent: Extent{MinX: 300, MinZ: 200, MaxX: 500, MaxZ: 240}}
	r := testRegion()
	r.Rivers = []River{testRiver()}
	a := mustClosure(t, r, ch)
	b := mustClosure(t, r, ch)
	if !reflect.DeepEqual(a, b) {
		t.Error("замыкание недетерминировано")
	}
}

// TestClosureGroupPartition — каждая пара (проекция, адрес) встречается в
// замыкании ровно один раз: группы не пересекаются, адреса в записи
// уникальны и упорядочены. Ключ — ПАРА, а не адрес: поверхность и покров
// законно делят один чанк, это разные проекции.
func TestClosureGroupPartition(t *testing.T) {
	res := mustClosure(t, testRegion(), Change{
		Kind:   SourcePath,
		Extent: Extent{MinX: 300, MinZ: 200, MaxX: 500, MaxZ: 240},
	})
	type key struct {
		p Projection
		a Address
	}
	seen := make(map[key]bool)
	for gi := range res.Groups {
		for ei := range res.Groups[gi].Entries {
			e := &res.Groups[gi].Entries[ei]
			prev := Address{Level: -2}
			for _, a := range e.Addresses {
				k := key{e.Projection, a}
				if seen[k] {
					t.Errorf("пара (%d, %+v) встречается дважды", int(e.Projection), a)
				}
				seen[k] = true
				if a.Level < prev.Level ||
					(a.Level == prev.Level && (a.CX < prev.CX || (a.CX == prev.CX && a.CZ <= prev.CZ && a != prev))) {
					t.Errorf("адреса не упорядочены: %+v после %+v", a, prev)
				}
				prev = a
			}
		}
	}
}

// TestClosureDegenerateExtent — точечный габарит не паникует и даёт конечный
// ответ: внутри чанка — по одному адресу на уровень, на границе чанков —
// обоих соседей по шву (замкнутые клетки) и ноль ячеек у полуоткрытых.
func TestClosureDegenerateExtent(t *testing.T) {
	r := testRegion()
	// Точка внутри чанка (0, 0) уровня 0: [300, 300].
	res := mustClosure(t, r, Change{
		Kind:   SourcePath,
		Extent: Extent{MinX: 300, MinZ: 300, MaxX: 300, MaxZ: 300},
	})
	surf := entryOf(t, groupOf(t, res, GroupSurface), Surface)
	if surf == nil {
		t.Fatal("поверхность отсутствует")
	}
	if len(surf.Addresses) != 5 {
		t.Errorf("точечная правка: поверхности адресов %d, ожидалось 5 (по одному на уровень)", len(surf.Addresses))
	}
	// Точка ровно на границе чанков: [256, 256] — оба соседа по шву.
	res = mustClosure(t, r, Change{
		Kind:   SourcePath,
		Extent: Extent{MinX: 256, MinZ: 256, MaxX: 256, MaxZ: 256},
	})
	surf = entryOf(t, groupOf(t, res, GroupSurface), Surface)
	if surf == nil {
		t.Fatal("поверхность отсутствует")
	}
	for _, want := range []Address{
		{Level: 0, CX: 0, CZ: 0}, {Level: 0, CX: 1, CZ: 0},
		{Level: 0, CX: 0, CZ: 1}, {Level: 0, CX: 1, CZ: 1},
	} {
		if !containsAddr(surf.Addresses, want) {
			t.Errorf("граничная точка не захватила соседа %+v", want)
		}
	}
	// Полуоткрытые клетки: на уровне 0 точка лежит ровно на углу четырёх
	// ячеек — площади нет, ячеек ноль; на уровнях 1+ она внутри клетки (0, 0)
	// и даёт по одной ячейке.
	res = mustClosure(t, r, Change{
		Kind:   SourceClearing,
		Extent: Extent{MinX: 256, MinZ: 256, MaxX: 256, MaxZ: 256},
	})
	cover := entryOf(t, groupOf(t, res, GroupVegetation), Cover)
	if cover == nil {
		t.Fatal("покров отсутствует")
	}
	if len(cover.Addresses) != 4 {
		t.Errorf("граничная точка: ячеек покрова %d, ожидалось 4 (по одной на уровнях 1–4)", len(cover.Addresses))
	}
	for _, a := range cover.Addresses {
		if a.Level == 0 {
			t.Errorf("на уровне 0 граничная точка дала ячейку %+v — площади нет", a)
		}
		if a.CX != 0 || a.CZ != 0 {
			t.Errorf("неожиданная ячейка покрова %+v", a)
		}
	}
}
