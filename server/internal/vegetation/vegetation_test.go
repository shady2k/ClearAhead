package vegetation

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
)

// Чанк (0, 0) уровня 0, покров целиком хвойный лес, рецепт — деревья в явном
// списке ячеек. Общая оснастка: тесты компилятора не зависят от рельефа, им
// нужен только блоб рецепта и покров, с которыми он обязан быть согласован.
func forestFixture(t *testing.T) ([]byte, []byte) {
	t.Helper()
	cover := make([]byte, chunk.CoverBytes)
	for k := range cover {
		cover[k] = chunk.PackCover(chunk.SurfaceForestConifer, 0)
	}
	recipe := make([]byte, chunk.ForestBytes)
	for _, c := range []struct{ i, j int }{
		{1, 1}, {2, 3}, {5, 5}, {10, 2}, {7, 8},
		{3, 3}, {12, 12}, {20, 0}, {0, 20}, {63, 63},
	} {
		chunk.SetForestOccupied(recipe, c.i, c.j)
	}
	return recipe, cover
}

// address — адрес чанка (0, 0) уровня 0, общий для всех проверок компилятора.
func address() chunk.Address { return chunk.Address{Level: chunk.ForestLevel} }

// Пустое множество исходников — база: проекция в точности равна лесу рецепта.
func TestProjectNoSourcesIsRecipe(t *testing.T) {
	recipe, cover := forestFixture(t)
	got, err := Project(address(), recipe, cover, Sources{})
	if err != nil {
		t.Fatalf("проекция: %v", err)
	}
	if !bytes.Equal(got, recipe) {
		t.Fatal("проекция без исходников изменила лес рецепта")
	}
}

// ПРИЁМОЧНЫЙ КРИТЕРИЙ: срубленное дерево не воскресает. Пересборка проекции из
// рецепта и тех же исходников обязана дать тот же блоб — рецепт не знает о
// рубке, и воскрешение означало бы, что вырубка потеряна между компиляциями.
func TestCutTreeDoesNotResurrect(t *testing.T) {
	recipe, cover := forestFixture(t)
	srcs := Sources{Cuts: []Cut{{CX: 0, CZ: 0, I: 2, J: 3}}}
	first, err := Project(address(), recipe, cover, srcs)
	if err != nil {
		t.Fatalf("проекция: %v", err)
	}
	if chunk.ForestOccupied(first, 2, 3) {
		t.Fatal("срубленное дерево осталось в проекции")
	}
	second, err := Project(address(), recipe, cover, srcs)
	if err != nil {
		t.Fatalf("пересборка: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("пересборка из рецепта изменила проекцию — дерево воскресло или сосед сдвинулся")
	}
}

// Дефект, ради которого отменена правка сомкнутости: уцелевшие деревья
// прыгают. Здесь рубка меняет РОВНО одну ячейку, и это проверяется перебором —
// любая схема, меняющая вход генератора, сдвинула бы и соседей.
func TestSurvivingTreesDoNotMove(t *testing.T) {
	recipe, cover := forestFixture(t)
	srcs := Sources{Cuts: []Cut{{CX: 0, CZ: 0, I: 2, J: 3}}}
	out, err := Project(address(), recipe, cover, srcs)
	if err != nil {
		t.Fatalf("проекция: %v", err)
	}
	for j := range chunk.CoverCells {
		for i := range chunk.CoverCells {
			want := chunk.ForestOccupied(recipe, i, j) && !(i == 2 && j == 3)
			if chunk.ForestOccupied(out, i, j) != want {
				t.Fatalf("ячейка (%d,%d): занятость %v, ожидалась %v — сосед сдвинулся", i, j,
					chunk.ForestOccupied(out, i, j), want)
			}
		}
	}
}

// Рубка ячейки без дерева — no-op, а не отказ: областная вырубка покрывает и
// пустые ячейки, и две формы одного факта обязаны совпасть; повторная рубка
// уже срубленного — идемпотентная команда.
func TestCutOfEmptyCellIsNoop(t *testing.T) {
	recipe, cover := forestFixture(t)
	srcs := Sources{Cuts: []Cut{{CX: 0, CZ: 0, I: 40, J: 40}}}
	out, err := Project(address(), recipe, cover, srcs)
	if err != nil {
		t.Fatalf("рубка пустой ячейки отказала: %v", err)
	}
	if !bytes.Equal(out, recipe) {
		t.Fatal("рубка пустой ячейки изменила проекцию")
	}
}

// Две формы одного факта: областная вырубка и поштучные надгробия на той же
// площади дают один блоб. Область [22, 34]² в координатах региона накрывает
// центры ячеек 5..8 по обеим осям (центр ячейки i — 4i+2), включая и пустые.
func TestAreaCutMatchesGraves(t *testing.T) {
	recipe, cover := forestFixture(t)
	area := Sources{Clearings: []Clearing{{MinX: 22, MinZ: 22, MaxX: 34, MaxZ: 34}}}
	graves := Sources{}
	for j := 5; j <= 8; j++ {
		for i := 5; i <= 8; i++ {
			graves.Cuts = append(graves.Cuts, Cut{CX: 0, CZ: 0, I: i, J: j})
		}
	}
	fromArea, err := Project(address(), recipe, cover, area)
	if err != nil {
		t.Fatalf("область: %v", err)
	}
	fromGraves, err := Project(address(), recipe, cover, graves)
	if err != nil {
		t.Fatalf("надгробия: %v", err)
	}
	if !bytes.Equal(fromArea, fromGraves) {
		t.Fatal("область и поштучные рубки разошлись на одной площади")
	}
	for j := 5; j <= 8; j++ {
		for i := 5; i <= 8; i++ {
			if chunk.ForestOccupied(fromArea, i, j) {
				t.Fatalf("дерево (%d,%d) внутри области вырубки уцелело", i, j)
			}
		}
	}
}

// Плотная маска и поштучные надгробия — тоже две формы одного факта.
func TestMaskCutMatchesGraves(t *testing.T) {
	recipe, cover := forestFixture(t)
	mask := NewCutMask(0, 0)
	graves := Sources{}
	for j := 5; j <= 8; j++ {
		for i := 5; i <= 8; i++ {
			if err := mask.Add(i, j); err != nil {
				t.Fatalf("маска: %v", err)
			}
			graves.Cuts = append(graves.Cuts, Cut{CX: 0, CZ: 0, I: i, J: j})
		}
	}
	fromMask, err := Project(address(), recipe, cover, Sources{CutMasks: []CutMask{*mask}})
	if err != nil {
		t.Fatalf("маска: %v", err)
	}
	fromGraves, err := Project(address(), recipe, cover, graves)
	if err != nil {
		t.Fatalf("надгробия: %v", err)
	}
	if !bytes.Equal(fromMask, fromGraves) {
		t.Fatal("маска и поштучные рубки разошлись")
	}
}

// Посаженный экземпляр переживает пересборку: посадка — исходник, а не побочный
// эффект рецепта, и обе компиляции обязаны поставить одно дерево.
func TestPlantedTreeSurvivesRebuild(t *testing.T) {
	recipe, cover := forestFixture(t)
	srcs := Sources{Planted: []Planted{{CX: 0, CZ: 0, I: 4, J: 4}}}
	first, err := Project(address(), recipe, cover, srcs)
	if err != nil {
		t.Fatalf("проекция: %v", err)
	}
	if !chunk.ForestOccupied(first, 4, 4) {
		t.Fatal("посаженное дерево не встало")
	}
	second, err := Project(address(), recipe, cover, srcs)
	if err != nil {
		t.Fatalf("пересборка: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("пересборка потеряла посаженное дерево")
	}
}

// Инвариант «бит только в лесном классе покрова»: рубка и посадка его не ломают.
// Покров с плешиной луга, посадка на лесной ячейке — проекция не ставит бит в
// луг; посадка прямо на луг — отказ, а не молчаливый лес на лугу.
func TestInvariantBitOnlyInForestClass(t *testing.T) {
	recipe, cover := forestFixture(t)
	cover[chunk.CoverIndex(30, 30)] = chunk.PackCover(chunk.SurfaceMeadow, 5)
	srcs := Sources{
		Cuts:    []Cut{{CX: 0, CZ: 0, I: 2, J: 3}},
		Planted: []Planted{{CX: 0, CZ: 0, I: 4, J: 4}},
	}
	out, err := Project(address(), recipe, cover, srcs)
	if err != nil {
		t.Fatalf("проекция: %v", err)
	}
	if chunk.ForestOccupied(out, 30, 30) {
		t.Fatal("бит встал в ячейке луга")
	}
	for j := range chunk.CoverCells {
		for i := range chunk.CoverCells {
			if !chunk.ForestOccupied(out, i, j) {
				continue
			}
			class, _ := chunk.UnpackCover(cover[chunk.CoverIndex(i, j)])
			if class != chunk.SurfaceForestConifer && class != chunk.SurfaceForestBroad {
				t.Fatalf("бит в ячейке (%d,%d) класса %d — не лес", i, j, class)
			}
		}
	}

	bad := Sources{Planted: []Planted{{CX: 0, CZ: 0, I: 30, J: 30}}}
	if _, err := Project(address(), recipe, cover, bad); err == nil {
		t.Fatal("посадка на лугу принята — инвариант ломается молча")
	}
}

// Рубка и посадка одного адреса — отказ, а не разрешение порядком: формула
// аддитивна, и результат не вправе зависеть от того, кто применился раньше.
func TestPlantOnCutCellRefused(t *testing.T) {
	recipe, cover := forestFixture(t)
	srcs := Sources{
		Cuts:    []Cut{{CX: 0, CZ: 0, I: 2, J: 3}},
		Planted: []Planted{{CX: 0, CZ: 0, I: 2, J: 3}},
	}
	if _, err := Project(address(), recipe, cover, srcs); err == nil {
		t.Fatal("рубка и посадка одного дерева приняты")
	}
}

// Посадка туда, где рецепт уже дал дерево, — отказ: исходник лжёт о факте,
// который мир и так содержит.
func TestPlantOnOccupiedCellRefused(t *testing.T) {
	recipe, cover := forestFixture(t)
	srcs := Sources{Planted: []Planted{{CX: 0, CZ: 0, I: 2, J: 3}}}
	if _, err := Project(address(), recipe, cover, srcs); err == nil {
		t.Fatal("посадка на существующее дерево принята")
	}
}

// Исходники — множество: повторная доставка одной команды не меняет результат
// и не отказывает. Проверяется обеими формами разом.
func TestDuplicateSourcesAreIdempotent(t *testing.T) {
	recipe, cover := forestFixture(t)
	once := Sources{
		Cuts:    []Cut{{CX: 0, CZ: 0, I: 2, J: 3}},
		Planted: []Planted{{CX: 0, CZ: 0, I: 4, J: 4}},
	}
	twice := Sources{
		Cuts:    []Cut{once.Cuts[0], once.Cuts[0]},
		Planted: []Planted{once.Planted[0], once.Planted[0]},
	}
	fromOnce, err := Project(address(), recipe, cover, once)
	if err != nil {
		t.Fatalf("одна доставка: %v", err)
	}
	fromTwice, err := Project(address(), recipe, cover, twice)
	if err != nil {
		t.Fatalf("двойная доставка отказала: %v", err)
	}
	if !bytes.Equal(fromOnce, fromTwice) {
		t.Fatal("повторная доставка журнала изменила проекцию")
	}
}

// Валидация исходников: чужая ячейка, вырожденная область, битый блоб — отказ
// с числом, а не молчаливая подстановка.
func TestSourceValidationRefusals(t *testing.T) {
	recipe, cover := forestFixture(t)
	cases := []struct {
		name string
		srcs Sources
		want string
	}{
		{"рубка в чужом чанке", Sources{Cuts: []Cut{{CX: 1, CZ: 0, I: 2, J: 3}}}, "применена к чанку"},
		{"рубка вне сетки", Sources{Cuts: []Cut{{CX: 0, CZ: 0, I: chunk.CoverCells, J: 0}}}, "вне сетки"},
		{"посадка вне сетки", Sources{Planted: []Planted{{CX: 0, CZ: 0, I: 0, J: -1}}}, "вне сетки"},
		{"маска в чужом чанке", Sources{CutMasks: []CutMask{{CX: 0, CZ: 1, Bits: make([]byte, chunk.ForestBytes)}}}, "применена к чанку"},
		{"маска не того размера", Sources{CutMasks: []CutMask{{CX: 0, CZ: 0, Bits: make([]byte, 42)}}}, "маска вырубки 42 байт"},
		{"вырожденная область", Sources{Clearings: []Clearing{{MinX: 100, MaxX: 0, MinZ: 0, MaxZ: 100}}}, "вырождена"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Project(address(), recipe, cover, c.srcs)
			if err == nil {
				t.Fatal("принято")
			}
			if !bytes.Contains([]byte(err.Error()), []byte(c.want)) {
				t.Fatalf("отказ %q не называет причину %q", err, c.want)
			}
		})
	}
}

// Вырожденный прямоугольник Min == Max проходит сквозь проверку Min > Max и
// дал бы пустую вырубку молча: площадь ноль — ошибка автора, а не вырубка.
// Отказ по любой оси, текст прежний — «вырождена».
func TestDegenerateClearingMinEqualsMaxRefused(t *testing.T) {
	recipe, cover := forestFixture(t)
	for _, cl := range []Clearing{
		{MinX: 10, MaxX: 10, MinZ: 0, MaxZ: 10},  // ноль по x
		{MinX: 0, MaxX: 10, MinZ: 10, MaxZ: 10},  // ноль по z
		{MinX: 10, MaxX: 10, MinZ: 10, MaxZ: 10}, // точка
	} {
		_, err := Project(address(), recipe, cover, Sources{Clearings: []Clearing{cl}})
		if err == nil {
			t.Fatalf("область [%g, %g] × [%g, %g] принята", cl.MinX, cl.MaxX, cl.MinZ, cl.MaxZ)
		}
		if !strings.Contains(err.Error(), "вырождена") {
			t.Fatalf("отказ %q не называет вырожденность", err)
		}
	}
}

// NaN-граница проходит сравнение Min > Max (NaN > x ложно при любом x) и
// превратилась бы в молчаливую пустую вырубку — тот же класс, на котором проект
// уже горел. Отказ по каждой из четырёх границ, текст называет NaN.
func TestNaNInClearingRefused(t *testing.T) {
	recipe, cover := forestFixture(t)
	for _, cl := range []Clearing{
		{MinX: math.NaN(), MaxX: 10, MinZ: 0, MaxZ: 10},
		{MinX: 0, MaxX: math.NaN(), MinZ: 0, MaxZ: 10},
		{MinX: 0, MaxX: 10, MinZ: math.NaN(), MaxZ: 10},
		{MinX: 0, MaxX: 10, MinZ: 0, MaxZ: math.NaN()},
	} {
		_, err := Project(address(), recipe, cover, Sources{Clearings: []Clearing{cl}})
		if err == nil {
			t.Fatalf("область с NaN принята: [%g, %g] × [%g, %g]", cl.MinX, cl.MaxX, cl.MinZ, cl.MaxZ)
		}
		if !strings.Contains(err.Error(), "NaN") {
			t.Fatalf("отказ %q не называет NaN", err)
		}
	}
}

// Лес существует только на уровне 0 (chunk.ForestLevel). Пустые исходники на
// любом другом уровне — законная пустота, а не отказ: проекции нет, потому что
// леса нет. Пин против переусердствовавшего фикса: отказ не имеет права
// прийти туда, где нечего терять.
func TestProjectAboveForestLevelEmptySourcesIsEmpty(t *testing.T) {
	recipe, cover := forestFixture(t)
	for _, level := range []int{1, 2, chunk.MaxLevelLimit} {
		a := chunk.Address{Level: level}
		got, err := Project(a, recipe, cover, Sources{})
		if err != nil {
			t.Fatalf("уровень %d, пустые исходники: %v", level, err)
		}
		if got != nil {
			t.Fatalf("уровень %d: проекция дала %d байт", level, len(got))
		}
	}
}

// Непустые исходники на уровне выше нулевого — отказ, а не молчаливая потеря:
// рубка, поданная не на тот уровень, исчезла бы без следа. Текст называет
// уровень и число исходников, и считаются все четыре формы.
func TestProjectSourcesAboveForestLevelRefused(t *testing.T) {
	recipe, cover := forestFixture(t)
	got, err := Project(chunk.Address{Level: 1}, recipe, cover,
		Sources{Cuts: []Cut{{CX: 0, CZ: 0, I: 2, J: 3}}})
	if err == nil {
		t.Fatal("рубка на уровне 1 принята")
	}
	if got != nil {
		t.Fatalf("отказ вернул проекцию %d байт", len(got))
	}
	if !strings.Contains(err.Error(), "уровень 1") {
		t.Fatalf("отказ %q не называет уровень", err)
	}
	if !strings.Contains(err.Error(), "1 шт.") {
		t.Fatalf("отказ %q не называет число исходников", err)
	}
	multi := Sources{
		Cuts:      []Cut{{CX: 0, CZ: 0, I: 1, J: 1}},
		Planted:   []Planted{{CX: 0, CZ: 0, I: 2, J: 2}},
		Clearings: []Clearing{{MinX: 0, MinZ: 0, MaxX: 1, MaxZ: 1}},
		CutMasks:  []CutMask{{CX: 0, CZ: 0, Bits: make([]byte, chunk.ForestBytes)}},
	}
	if _, err := Project(chunk.Address{Level: 2}, recipe, cover, multi); err == nil {
		t.Fatal("четыре формы на уровне 2 приняты")
	}
}

// Карта без покрова не имеет леса — пустые исходники (nil И явно пустые срезы)
// дают пустую проекцию (то же правило, что у ChunkForest), а непустые — отказ:
// молча терять журналируемую рубку запрещено.
func TestProjectNilCoverIsEmpty(t *testing.T) {
	recipe, _ := forestFixture(t)
	got, err := Project(address(), recipe, nil, Sources{})
	if err != nil {
		t.Fatalf("проекция без покрова: %v", err)
	}
	if got != nil {
		t.Fatalf("проекция без покрова дала %d байт", len(got))
	}
	empty := Sources{Cuts: []Cut{}, Planted: []Planted{}, Clearings: []Clearing{}, CutMasks: []CutMask{}}
	if got, err := Project(address(), recipe, nil, empty); err != nil || got != nil {
		t.Fatalf("явно пустые исходники без покрова: %v, %v", got, err)
	}
	srcs := Sources{Cuts: []Cut{{CX: 0, CZ: 0, I: 2, J: 3}}}
	if _, err := Project(address(), recipe, nil, srcs); err == nil {
		t.Fatal("рубка на карте без покрова принята")
	}
}

// Маска умеет различать форму множества вырубки: ячейка, помеченная дважды, —
// одна ячейка.
func TestCutMaskAddAndContains(t *testing.T) {
	m := NewCutMask(3, -2)
	if m.CX != 3 || m.CZ != -2 {
		t.Fatalf("маска несёт чанк (%d,%d), ожидался (3,-2)", m.CX, m.CZ)
	}
	if len(m.Bits) != chunk.ForestBytes {
		t.Fatalf("маска %d байт, ожидалось %d", len(m.Bits), chunk.ForestBytes)
	}
	if err := m.Add(1, 1); err != nil {
		t.Fatalf("маска: %v", err)
	}
	if err := m.Add(1, 1); err != nil {
		t.Fatalf("повторная отметка: %v", err)
	}
	if !m.Contains(1, 1) {
		t.Fatal("бит не встал")
	}
	if m.Contains(1, 2) {
		t.Fatal("соседняя ячейка помечена")
	}
}

// Адрес вне сетки в Add — отказ, а не молчание: маску строит вызывающий по
// своим координатам, и потерянная молча рубка исчезла бы без следа. После
// отказа бит не встаёт ни в одной ячейке.
func TestCutMaskAddOutOfBoundsRefused(t *testing.T) {
	m := NewCutMask(0, 0)
	for _, cell := range [][2]int{
		{-1, 0}, {0, -1},
		{chunk.CoverCells, 0}, {0, chunk.CoverCells},
		{chunk.CoverCells, chunk.CoverCells},
	} {
		if err := m.Add(cell[0], cell[1]); err == nil {
			t.Fatalf("ячейка (%d,%d) принята", cell[0], cell[1])
		}
	}
	for j := 0; j < chunk.CoverCells; j++ {
		for i := 0; i < chunk.CoverCells; i++ {
			if m.Contains(i, j) {
				t.Fatalf("бит встал после отказа: (%d,%d)", i, j)
			}
		}
	}
}
