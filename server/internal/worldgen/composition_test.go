// Приёмка sqym.18 через КОМПОЗИЦИЮ, а не перенос переменной: хранилище
// исходников + сервис правок + конвейер мира переживают НАСТОЯЩИЙ перезапуск
// (сервис создаётся заново над тем же файлом, база проекций снесена), и мир
// пересобирается вместе с правками игрока. Это ровно те вызовы, что собирает
// cmd/clearahead: NewServiceStored -> Bootstrap -> GenerateSources ->
// NewLazySources -> NewCompilerSources.
package worldgen

import (
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/edit"
	"github.com/shady2k/ClearAhead/server/internal/project"
	"github.com/shady2k/ClearAhead/server/internal/sourcestore"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/uuidv7"
	"github.com/shady2k/ClearAhead/server/internal/vegetation"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// provenance — происхождение региона в тестах композиции: то же, что main
// кладёт в бутстрап.
const provenance = `{"source":"seedmap","kind":"fixture"}`

// assertGradedPlateau — все отсчёты чанка лежат ровно на отметке правки:
// правка применена, а не просочилась в часть клетки.
func assertGradedPlateau(t *testing.T, c worldstore.Chunk, want int16, what string) {
	t.Helper()
	for j := range chunk.Samples - 1 {
		for i := range chunk.Samples - 1 {
			if got := c.Heights[chunk.Index(i, j)]; got != want {
				t.Fatalf("%s: отсчёт (%d, %d) = %d см, ожидалось %d", what, i, j, got, want)
			}
		}
	}
}

// TestGradingSurvivesRestartThroughComposition — ПРИЁМОЧНЫЙ КРИТЕРИЙ sqym.18:
// правка высот переживает НАСТОЯЩИЙ перезапуск с пересевом. Сервис правок
// создаётся заново над тем же файлом исходников (вторая жизнь), база проекций
// — свежая (пересев), и мир пересобирается из закоммиченного: правка на
// месте, чанк с ней байт в байт тот же, что до перезапуска, и прогрев, и
// порождение по требованию, и пересборка замыкания знают её.
func TestGradingSurvivesRestartThroughComposition(t *testing.T) {
	dir := t.TempDir()
	m := newMap(t)
	graded := chunk.Address{Region: m.MapID, Level: 0, CX: 0, CZ: -2}

	// ПЕРВАЯ ЖИЗНЬ: коммит правки кладёт закоммиченное в хранилище исходников.
	sources, err := sourcestore.Open(filepath.Join(dir, "sources.db"))
	if err != nil {
		t.Fatalf("хранилище исходников: %v", err)
	}
	svc, err := edit.NewServiceStored(sources, m, uuidv7.Deterministic())
	if err != nil {
		t.Fatalf("сервис правок: %v", err)
	}
	sess, err := svc.OpenSession("b1")
	if err != nil {
		t.Fatalf("сессия: %v", err)
	}
	if _, err := sess.Apply(edit.Intent{Op: edit.OpGrade, Grade: edit.GradeIntent{
		Cells: []terrain.GradeCell{gradedCell(0, -2, 500)},
	}}); err != nil {
		t.Fatalf("правка высот: %v", err)
	}
	if err := sess.Commit(); err != nil {
		t.Fatalf("коммит: %v", err)
	}
	committed := svc.Committed()
	src := Sources{Grading: svc.Grading()}
	if err := sources.Close(); err != nil {
		t.Fatal(err)
	}

	// Прогрев первой жизни над базой проекций №1.
	w1, err := worldstore.Open(filepath.Join(dir, "world1.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Bootstrap(w1, &committed, 1, provenance); err != nil {
		t.Fatalf("бутстрап: %v", err)
	}
	if _, err := GenerateSources(w1, &committed, src, m.MapID, 1, 1); err != nil {
		t.Fatalf("прогрев: %v", err)
	}
	before := chunkAtAddr(t, w1, graded, 1)
	assertGradedPlateau(t, before, 500, "до перезапуска")
	if err := w1.Close(); err != nil {
		t.Fatal(err)
	}

	// ВТОРАЯ ЖИЗНЬ — ПЕРЕЗАПУСК: новое хранилище над тем же файлом, НОВЫЙ
	// сервис правок (переменная не переносится), база проекций снесена.
	sources2, err := sourcestore.Open(filepath.Join(dir, "sources.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sources2.Close()
	svc2, err := edit.NewServiceStored(sources2, m, uuidv7.Deterministic())
	if err != nil {
		t.Fatalf("сервис правок после перезапуска: %v", err)
	}
	if seq := svc2.JournalSeq(); seq != 1 {
		t.Fatalf("журнал после перезапуска %d, ожидалась 1", seq)
	}
	committed2 := svc2.Committed()
	src2 := Sources{Grading: svc2.Grading()}

	w2, err := worldstore.Open(filepath.Join(dir, "world2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()
	if _, _, err := Bootstrap(w2, &committed2, 1, provenance); err != nil {
		t.Fatalf("бутстрап после перезапуска: %v", err)
	}
	if _, err := GenerateSources(w2, &committed2, src2, m.MapID, 1, 1); err != nil {
		t.Fatalf("пересев: %v", err)
	}
	after := chunkAtAddr(t, w2, graded, 1)
	if !chunkEq(before, after) {
		t.Fatal("пересев изменил землю: чанк правки до и после перезапуска разошёлся — правка не пережила перезапуск")
	}
	assertGradedPlateau(t, after, 500, "после перезапуска")

	// Порождение по требованию после перезапуска знает правку.
	lazy, err := NewLazySources(w2, &committed2, src2, m.MapID, 1)
	if err != nil {
		t.Fatalf("порождение: %v", err)
	}
	lc, ok, err := lazy.MakeChunk(graded, 1)
	if err != nil || !ok {
		t.Fatalf("MakeChunk: ok=%v err=%v", ok, err)
	}
	assertGradedPlateau(t, lc, 500, "ленивое порождение после перезапуска")

	// Пересборка замыкания после перезапуска знает правку: боевой путь
	// пересборки (worldgen.Compiler) строит мир НАД исходниками.
	cc := NewCompilerSources(w2, m.MapID, 1, svc2.JournalSeq(), src2)
	minX, minZ, maxX, maxZ := terrain.GradeCell{Level: 0, CX: 0, CZ: -2}.Bounds()
	ch := project.Change{Kind: project.SourceGrading,
		Extent: project.Extent{MinX: minX, MinZ: minZ, MaxX: maxX, MaxZ: maxZ}}
	if _, err := cc.Rebuild(&committed2, ch); err != nil {
		t.Fatalf("пересборка: %v", err)
	}
	reb := chunkAtAddr(t, w2, graded, publishedVersion(t, w2))
	assertGradedPlateau(t, reb, 500, "пересборка замыкания после перезапуска")
}

// TestCutTreeDoesNotResurrectThroughComposition — срубленное дерево не
// воскресает БОЕВЫМ ПУТЁМ: вырубка лежит в хранилище исходников, и лес
// собирается проекцией (vegetation.Project) в прогреве, порождении по
// требованию и пересборке замыкания. Уцелевшие деревья при этом не сдвинулись
// — проекция отличается от рецепта РОВНО срубленной ячейкой.
func TestCutTreeDoesNotResurrectThroughComposition(t *testing.T) {
	dir := t.TempDir()
	m := newMap(t)

	// Цель рубки ищется СРЕДИ ПОРОЖДЁННЫХ чанков, а не на поле: прогрев хранит
	// не все клетки окрестности (правило покрытия), и чанк с лесом обязан быть
	// в базе — иначе рубке не во что лечь. Рецепт — лес, записанный прогревом
	// БЕЗ вырубки: тот же блоб, что дал бы field.ChunkForest (детерминизм §3).
	w1, err := worldstore.Open(filepath.Join(dir, "world1.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Bootstrap(w1, m, 1, provenance); err != nil {
		t.Fatalf("бутстрап: %v", err)
	}
	if _, err := GenerateSources(w1, m, Sources{}, m.MapID, 1, 1); err != nil {
		t.Fatalf("прогрев без вырубки: %v", err)
	}
	a, recipe, cut := storedForestChunk(t, w1)

	veg := vegetation.Sources{Cuts: []vegetation.Cut{{CX: a.CX, CZ: a.CZ, I: cut[0], J: cut[1]}}}
	src := Sources{Vegetation: veg}

	// Прогрев боевым путём с вырубкой: строка чанка перезаписывается проекцией.
	if _, err := GenerateSources(w1, m, src, m.MapID, 1, 1); err != nil {
		t.Fatalf("прогрев с вырубкой: %v", err)
	}
	first := chunkAtAddr(t, w1, a, 1)
	assertOneTreeCut(t, first.Forest, recipe, cut, "прогрев")

	// Пересев: база проекций снесена, исходники те же — срубленное не
	// воскресло, уцелевшие не сдвинулись.
	w2, err := worldstore.Open(filepath.Join(dir, "world2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()
	if _, _, err := Bootstrap(w2, m, 1, provenance); err != nil {
		t.Fatalf("бутстрап после пересева: %v", err)
	}
	if _, err := GenerateSources(w2, m, src, m.MapID, 1, 1); err != nil {
		t.Fatalf("пересев: %v", err)
	}
	second := chunkAtAddr(t, w2, a, 1)
	if !chunkEq(first, second) {
		t.Fatal("пересев изменил лес: проекция до и после разошлась — вырубка не пережила пересев")
	}

	// Пересборка замыкания боевым путём: вырубка входит в замыкание
	// (SourceClearing -> покров), и пересобранный лес обязан остаться без
	// срубленного.
	cc := NewCompilerSources(w2, m.MapID, 1, 0, src)
	ox, oz := a.OriginM()
	side := chunk.SideM(0)
	ch := project.Change{Kind: project.SourceClearing,
		Extent: project.Extent{MinX: ox, MinZ: oz, MaxX: ox + side, MaxZ: oz + side}}
	if _, err := cc.Rebuild(m, ch); err != nil {
		t.Fatalf("пересборка: %v", err)
	}
	reb := chunkAtAddr(t, w2, a, publishedVersion(t, w2))
	assertOneTreeCut(t, reb.Forest, recipe, cut, "пересборка замыкания")

	// Порождение по требованию знает вырубку.
	lazy, err := NewLazySources(w2, m, src, m.MapID, 1)
	if err != nil {
		t.Fatalf("порождение: %v", err)
	}
	lc, ok, err := lazy.MakeChunk(a, publishedVersion(t, w2))
	if err != nil || !ok {
		t.Fatalf("MakeChunk: ok=%v err=%v", ok, err)
	}
	assertOneTreeCut(t, lc.Forest, recipe, cut, "ленивое порождение")
}

// storedForestChunk ищет среди ПОРОЖДЁННЫХ прогревом чанков уровня 0 такой,
// чей лес непуст, и возвращает его адрес, лес-рецепт и ячейку первого дерева.
// Поиск по базе, а не по полю: рубить можно только то, что мир реально хранит
// (правило покрытия), и эталон рецепта — записанный блоб, а не пересчитанный.
func storedForestChunk(t *testing.T, s *worldstore.Store) (chunk.Address, []byte, [2]int) {
	t.Helper()
	for cz := -4; cz <= 4; cz++ {
		for cx := -4; cx <= 4; cx++ {
			a := chunk.Address{Region: "ST_A", Level: chunk.ForestLevel, CX: cx, CZ: cz}
			c, ok, err := s.GetChunk(a, 1)
			if err != nil || !ok {
				continue
			}
			for j := range chunk.CoverCells {
				for i := range chunk.CoverCells {
					if chunk.ForestOccupied(c.Forest, i, j) {
						return a, c.Forest, [2]int{i, j}
					}
				}
			}
		}
	}
	t.Fatal("среди порождённых чанков леса не нашлось")
	return chunk.Address{}, nil, [2]int{}
}

// assertOneTreeCut — в проекции срублена РОВНО ячейка cut: срубленное дерево
// отсутствует, уцелевшие не сдвинулись (проекция и рецепт различаются одной
// ячейкой).
func assertOneTreeCut(t *testing.T, projected, recipe []byte, cut [2]int, what string) {
	t.Helper()
	if chunk.ForestOccupied(projected, cut[0], cut[1]) {
		t.Fatalf("%s: срубленное дерево осталось в проекции", what)
	}
	diff := 0
	for j := range chunk.CoverCells {
		for i := range chunk.CoverCells {
			if chunk.ForestOccupied(projected, i, j) != chunk.ForestOccupied(recipe, i, j) {
				diff++
				if i != cut[0] || j != cut[1] {
					t.Fatalf("%s: уцелевшее дерево (%d, %d) сдвинулось", what, i, j)
				}
			}
		}
	}
	if diff != 1 {
		t.Fatalf("%s: проекция отличается от рецепта %d ячейками, ожидалась ровно 1 (срубленное)", what, diff)
	}
}
