package worldgen

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/project"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// editApproach — правка пути: подход удлиняется на 8 м, и весь узел горловины
// (обе стрелки и главный путь) сдвигается на восток. Изменённая площадь —
// коридоры стрелок и конец главного пути; габарит правки обязан накрыть их
// ВСЕ (контракт: габарит полный и консервативный — лишний чанк на пересборку
// дешевле пропущенного шва).
func editApproach(t *testing.T) *mapfmt.Map {
	t.Helper()
	m := seedmap.Station(seedmap.WithTerrain(), seedmap.Mutate(func(m *mapfmt.Map) {
		g := m.Geometry.Edges[seedmap.StationApproach]
		g.Horizontal[0].Length = 128
		m.Geometry.Edges[seedmap.StationApproach] = g
		// Run решётки покрывает подход до его конца: правка длины без правки
		// спана дала бы «покрытие кончается на 120, ожидается 128» — валидатор
		// отказывает, а не чинит.
		for i := range m.Construction.Runs {
			for j := range m.Construction.Runs[i].Spans {
				s := &m.Construction.Runs[i].Spans[j]
				if s.Element == seedmap.StationApproach {
					s.To = 128
				}
			}
		}
	}))
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("правка не прошла валидацию: %v", err)
	}
	return m
}

// changeExtent — габарит правки в плане (X на восток, Z на север): горловина
// от подхода до конца главного пути и коридоры обеих стрелок. Коснулся
// границ X=256 и Z=0 — шовные соседи обязаны войти в замыкание.
var changeExtent = project.Extent{MinX: 100, MinZ: -70, MaxX: 400, MaxZ: 12}

// pathChange — правка пути с этим габаритом.
var pathChange = project.Change{Kind: project.SourcePath, Extent: changeExtent}

// rebuildFixture — база со СТАРЫМ миром (полный прогрев исходной карты, версия
// 1) и компилятор над ней.
func rebuildFixture(t *testing.T) (*Compiler, *worldstore.Store) {
	t.Helper()
	s := newStore(t)
	m := newMap(t)
	// Голова проекций обязана существовать: пересборка публикует под НОВОЙ
	// версией (worldstore.Publish). Бутстрап идемпотентен — newStore уже завёл
	// регион, и Seed дописывает голову версии 1 с первой сетью.
	if _, _, err := Bootstrap(s, m, 1, `{"source":"seedmap","kind":"fixture"}`); err != nil {
		t.Fatalf("бутстрап: %v", err)
	}
	if _, err := Generate(s, m, "ST_A", 1, 1); err != nil {
		t.Fatalf("старый мир: %v", err)
	}
	return NewCompiler(s, "ST_A", 1, 0), s
}

// publishedVersion — версия мира после пересборки: голова назвала её в момент
// публикации, и читать строки замыкания обязаны под ней.
func publishedVersion(t *testing.T, s *worldstore.Store) int64 {
	t.Helper()
	head, ok, err := s.GetProjectionHead("ST_A")
	if err != nil || !ok {
		t.Fatalf("голова проекций: ok=%v err=%v", ok, err)
	}
	return head.WorldVersion
}

// chunkEq — побайтовое равенство содержимого строки чанка (хеш и ревизия
// сюда не входят: эталон считан чистым Compile и хеша не несёт).
func chunkEq(a, b worldstore.Chunk) bool {
	if len(a.Heights) != len(b.Heights) {
		return false
	}
	for i := range a.Heights {
		if a.Heights[i] != b.Heights[i] {
			return false
		}
	}
	return bytes.Equal(a.Cover, b.Cover) && bytes.Equal(a.Forest, b.Forest)
}

// closureAddresses — множество адресов замыкания (без корня мира), сведённое
// по всем группам и проекциям: адрес, делящий несколько проекций, в множестве
// один. Регион подставляется тем же шагом, что у Compiler.Rebuild: адрес
// замыкания региона не несёт (project.Address).
func closureAddresses(t *testing.T, reg project.Region, ch project.Change) map[chunk.Address]bool {
	t.Helper()
	closure, err := reg.Closure(ch)
	if err != nil {
		t.Fatalf("замыкание: %v", err)
	}
	out := make(map[chunk.Address]bool)
	for _, plan := range closure.Groups {
		for _, entry := range plan.Entries {
			for _, a := range entry.Addresses {
				if a.Level >= 0 {
					a.Region = "ST_A"
					out[a] = true
				}
			}
		}
	}
	return out
}

// TestRebuildMatchesClosureAndFullCompile — правка одной стрелки пересобирает
// РОВНО замыкание (сравнение с project.Closure, а не с зашитым числом) и ни
// одним адресом больше: каждый адрес замыкания после пересборки несёт байт в
// байт тот чанк, что дала полная компиляция карты с правкой, а каждый адрес
// ВНЕ замыкания не тронут (байт в байт старый мир). Полный проход — эталон
// детерминизма §3 контракта.
func TestRebuildMatchesClosureAndFullCompile(t *testing.T) {
	cc, s := rebuildFixture(t)
	edited := editApproach(t)

	oldWorld := compileByAddress(t, cc, newMap(t))
	newWorld := compileByAddress(t, cc, edited)

	reg, err := regionOf(edited)
	if err != nil {
		t.Fatalf("регион: %v", err)
	}
	want := closureAddresses(t, reg, pathChange)
	if len(want) == 0 {
		t.Fatal("замыкание пусто")
	}

	res, err := cc.Rebuild(edited, pathChange)
	if err != nil {
		t.Fatalf("пересборка: %v", err)
	}
	// Строки замыкания лежат под ВЕРСИЕЙ ПУБЛИКАЦИИ, а не под старой:
	// чтение по адресу без версии брало бы прежний мир.
	rebV := publishedVersion(t, s)
	if res.TotalChunks != len(want) {
		t.Fatalf("пересобрано %d адресов, замыкание даёт %d", res.TotalChunks, len(want))
	}

	// Каждый адрес замыкания пересобран байт в байт, как полным проходом.
	changed := 0
	for a := range want {
		stored, ok, err := s.GetChunk(a, rebV)
		if err != nil || !ok {
			t.Fatalf("чанк %v после пересборки: ok=%v err=%v", a, ok, err)
		}
		ref, ok := newWorld[a]
		if !ok {
			t.Fatalf("полный проход правки не породил адрес %v замыкания", a)
		}
		if !chunkEq(stored, ref) {
			t.Fatalf("чанк %v пересобран не как полный проход", a)
		}
		if old, ok := oldWorld[a]; ok && !chunkEq(old, ref) {
			changed++
		}
	}
	if changed == 0 {
		t.Fatal("правка не изменила ни одного чанка замыкания — фикстура правки выродилась")
	}

	// Ни одним адресом больше: адреса вне замыкания лежат байт в байт, как
	// были посчитаны полным проходом исходной карты.
	for a, old := range oldWorld {
		if want[a] {
			continue
		}
		stored, ok, err := s.GetChunk(a, rebV)
		if err != nil || !ok {
			t.Fatalf("чанк %v вне замыкания потерян: ok=%v err=%v", a, ok, err)
		}
		if !chunkEq(stored, old) {
			t.Fatalf("чанк %v вне замыкания перезаписан — пересобрано больше, чем велит замыкание", a)
		}
	}

	// Патчи построены на ВСЕХ уровнях, а не только на нулевом (§5.3): выемка
	// не должна исчезать при отдалении камеры.
	perLevel := map[int]int{}
	for a := range want {
		perLevel[a.Level]++
	}
	for level := 0; level <= reg.Rule.MaxLevel; level++ {
		if perLevel[level] == 0 {
			t.Fatalf("замыкание не задело уровень %d — патч на нём не построен", level)
		}
	}

	// Исход каждой проекции назван: сеть — другой писатель, геометрия —
	// клиентская производная, остальные пересобраны.
	outcome := map[project.Projection]Outcome{}
	for _, gr := range res.Groups {
		for _, e := range gr.Entries {
			outcome[e.Projection] = e.Outcome
		}
	}
	if outcome[project.Network] != OutcomeRebuilt {
		t.Fatalf("сеть: исход %d, ожидался другой писатель", outcome[project.Network])
	}
	if outcome[project.Geometry] != OutcomeClientDerived {
		t.Fatalf("геометрия: исход %d, ожидалась клиентская производная", outcome[project.Geometry])
	}
	for _, p := range []project.Projection{project.Surface, project.Cover, project.Vegetation} {
		if outcome[p] != OutcomeRebuilt {
			t.Fatalf("проекция %v: исход %d, ожидалась пересборка", p, outcome[p])
		}
	}
}

// TestRebuildSeamMatches — шовный инвариант (§5.1) после адресной пересборки:
// общий ряд отсчётов двух соседних чанков совпадает байт в байт. Габарит
// правки коснулся границ X=256 и Z=0, и замыкание включило обоих соседей по
// каждой границе (замкнутые клетки, project).
func TestRebuildSeamMatches(t *testing.T) {
	cc, s := rebuildFixture(t)
	edited := editApproach(t)
	if _, err := cc.Rebuild(edited, pathChange); err != nil {
		t.Fatalf("пересборка: %v", err)
	}
	// Шов читается по ОПУБЛИКОВАННОЙ версии: строки замыкания лежат под ней.
	rebV := publishedVersion(t, s)

	reg, err := regionOf(edited)
	if err != nil {
		t.Fatalf("регион: %v", err)
	}
	want := closureAddresses(t, reg, pathChange)
	read := func(a chunk.Address) worldstore.Chunk {
		t.Helper()
		c, ok, err := s.GetChunk(a, rebV)
		if err != nil || !ok {
			t.Fatalf("чанк %v: ok=%v err=%v", a, ok, err)
		}
		return c
	}

	checked := 0
	for a := range want {
		if a.Level != 0 {
			continue
		}
		rightAddr := chunk.Address{Region: a.Region, Level: a.Level, CX: a.CX + 1, CZ: a.CZ}
		if want[rightAddr] {
			left, rightCh := read(a), read(rightAddr)
			for j := range chunk.Samples {
				if left.Heights[chunk.Index(chunk.Samples-1, j)] != rightCh.Heights[chunk.Index(0, j)] {
					t.Fatalf("шов по X у %v: ряд %d разошёлся: %d против %d",
						a, j, left.Heights[chunk.Index(chunk.Samples-1, j)], rightCh.Heights[chunk.Index(0, j)])
				}
			}
			checked++
		}
		upperAddr := chunk.Address{Region: a.Region, Level: a.Level, CX: a.CX, CZ: a.CZ + 1}
		if want[upperAddr] {
			lower, upperCh := read(a), read(upperAddr)
			for i := range chunk.Samples {
				if lower.Heights[chunk.Index(i, chunk.Samples-1)] != upperCh.Heights[chunk.Index(i, 0)] {
					t.Fatalf("шов по Z у %v: столбец %d разошёлся: %d против %d",
						a, i, lower.Heights[chunk.Index(i, chunk.Samples-1)], upperCh.Heights[chunk.Index(i, 0)])
				}
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("ни одной шовной пары в замыкании — габарит правки не коснулся границ")
	}
}

// TestPreviewWritesNothing — предпросмотр не пересобирает ничего: ноль записей
// в базу. Разделение «предпросмотр без пересборки, авторитетный пересчёт по
// подтверждению» (спека §8.1) держится этим замером.
func TestPreviewWritesNothing(t *testing.T) {
	cc, s := rebuildFixture(t)
	edited := editApproach(t)

	before := countChunks(t, s)
	res, err := cc.Preview(edited, pathChange)
	if err != nil {
		t.Fatalf("предпросмотр: %v", err)
	}
	after := countChunks(t, s)
	if before != after {
		t.Fatalf("предпросмотр записал %d строк в базу", after-before)
	}

	// Отчёт предпросмотра — тот же, что дал бы авторитетный пересчёт: разница
	// только в записи.
	resR, err := cc.Rebuild(edited, pathChange)
	if err != nil {
		t.Fatalf("пересборка: %v", err)
	}
	if !reflect.DeepEqual(res, resR) {
		t.Fatalf("отчёты предпросмотра и пересборки разошлись:\nпредпросмотр: %+v\nпересборка:  %+v", res, resR)
	}
	// Версии мира — разные строки: пересборка легла РЯДОМ со старым миром,
	// и счёт вырос ровно на размер замыкания (по адресу — одна строка).
	if grew := countChunks(t, s) - after; grew != resR.TotalChunks {
		t.Fatalf("пересборка записала %d строк, замыкание даёт %d", grew, resR.TotalChunks)
	}
}

// TestRebuildOutcomeCategories — перечень проекций без серверной
// материализации ПРИБИТ: категория исхода каждой адресуемой проекции
// зафиксирована, и молчаливая смена категории (например, почанковая вода)
// роняет тест и заставляет обновить и пересборку, и объявление.
func TestRebuildOutcomeCategories(t *testing.T) {
	pinned := map[project.Projection]Outcome{
		project.Network:    OutcomeRebuilt,         // сеть публикуется здесь (sqym.5)
		project.Surface:    OutcomeRebuilt,         // высоты в строке чанка
		project.Cover:      OutcomeRebuilt,         // покров в строке чанка
		project.Vegetation: OutcomeRebuilt,         // лес в строке чанка
		project.Water:      OutcomeNotMaterialized, // формы ещё нет — гидрология не реализована
		project.Geometry:   OutcomeClientDerived,   // тесселирует клиент (§5.4)
	}
	cc, _ := rebuildFixture(t)
	edited := editApproach(t)

	// Правка пути вводит в замыкание всё, кроме воды; правка реки — воду.
	changes := []project.Change{
		pathChange,
		{Kind: project.SourceRiver, Extent: project.Extent{MinX: -100, MinZ: 350, MaxX: 100, MaxZ: 500}},
	}
	for _, ch := range changes {
		res, err := cc.Preview(edited, ch)
		if err != nil {
			t.Fatalf("предпросмотр правки %v: %v", ch.Kind, err)
		}
		for _, gr := range res.Groups {
			for _, e := range gr.Entries {
				want, ok := pinned[e.Projection]
				if !ok {
					t.Fatalf("проекция %v не прибита в перечне категорий", e.Projection)
				}
				if e.Outcome != want {
					t.Fatalf("проекция %v: исход %d, прибит %d — обнови пересборку И объявление",
						e.Projection, e.Outcome, want)
				}
				if e.Outcome == OutcomeRebuilt && e.Rebuilt == 0 {
					t.Fatalf("проекция %v объявлена пересобранной с нулём адресов", e.Projection)
				}
				if e.Outcome != OutcomeRebuilt && e.Reason == "" {
					t.Fatalf("проекция %v: исход без причины", e.Projection)
				}
			}
		}
	}
}

// compileByAddress — полный проход компилятора, проиндексированный по адресу.
func compileByAddress(t *testing.T, cc *Compiler, m *mapfmt.Map) map[chunk.Address]worldstore.Chunk {
	t.Helper()
	chunks, err := cc.Compile(m)
	if err != nil {
		t.Fatalf("полный проход: %v", err)
	}
	out := make(map[chunk.Address]worldstore.Chunk, len(chunks))
	for _, c := range chunks {
		out[c.Address] = c
	}
	return out
}

// countChunks — сколько строк лежит у региона на всех уровнях.
func countChunks(t *testing.T, s *worldstore.Store) int {
	t.Helper()
	total := 0
	for level := 0; level <= chunk.MaxLevelLimit; level++ {
		n, err := s.CountChunks("ST_A", level)
		if err != nil {
			t.Fatalf("счёт: %v", err)
		}
		total += n
	}
	return total
}

// TestRebuildPublishesNetworkWithSurfaceAtomically — публикация группы
// «сеть + поверхность» атомарна (sqym.5): одна пересборка даёт ОДНУ версию
// мира, под которой лежат и тело сети, и строки замыкания, а голова называет
// обе. Промежуточного состояния «сеть новая, земля старая» не существует:
// версия появляется целиком — worldstore.Publish пишет одной транзакцией.
func TestRebuildPublishesNetworkWithSurfaceAtomically(t *testing.T) {
	cc, s := rebuildFixture(t)
	head, _, err := s.GetProjectionHead("ST_A")
	if err != nil {
		t.Fatalf("голова: %v", err)
	}
	if head.WorldVersion != 1 || head.NetworkVersion != 1 {
		t.Fatalf("засев: мир %d, сеть %d — ожидались 1 и 1", head.WorldVersion, head.NetworkVersion)
	}
	// Правка пути вводит в замыкание и сеть, и поверхность.
	edited := editApproach(t)
	if _, err := cc.Rebuild(edited, pathChange); err != nil {
		t.Fatalf("пересборка: %v", err)
	}

	head, _, err = s.GetProjectionHead("ST_A")
	if err != nil {
		t.Fatalf("голова: %v", err)
	}
	if head.WorldVersion != 2 {
		t.Fatalf("версия мира %d, ожидалась 2 — пересборка не опубликовала версию", head.WorldVersion)
	}
	if head.NetworkVersion != 2 {
		t.Fatalf("версия сети %d, ожидалась 2 — сеть не вошла в публикацию", head.NetworkVersion)
	}

	// Тело сети под новой версией лежит и отдаётся; сеть версии 1 (засев)
	// осталась на месте — версии неизменны по адресу.
	body, hash, ok, err := s.GetNetwork("ST_A", 2)
	if err != nil || !ok {
		t.Fatalf("сеть версии 2: ok=%v err=%v", ok, err)
	}
	if len(body) == 0 || len(hash) != 64 {
		t.Fatalf("тело сети %d байт, хеш %d символов", len(body), len(hash))
	}
	if _, _, ok, err := s.GetNetwork("ST_A", 1); err != nil || !ok {
		t.Fatalf("сеть версии 1 после пересборки: ok=%v err=%v", ok, err)
	}

	// Строки замыкания читаются под той же версией, что и сеть: согласованная
	// пара «сеть + поверхность» доступна одним адресом версии.
	reg, err := regionOf(edited)
	if err != nil {
		t.Fatalf("регион: %v", err)
	}
	want := closureAddresses(t, reg, pathChange)
	for a := range want {
		if _, ok, err := s.GetChunk(a, 2); err != nil || !ok {
			t.Fatalf("чанк %v версии 2: ok=%v err=%v", a, ok, err)
		}
	}
}
