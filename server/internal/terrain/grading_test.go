package terrain

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// buildGraded — поле с правками: тот же полный путь входа, что buildField,
// плюс правки как отдельный исходник.
func buildGraded(t *testing.T, m *mapfmt.Map, g Grading) *Field {
	t.Helper()
	_, els, err := track.Propagate(m)
	if err != nil {
		t.Fatalf("распространение поз: %v", err)
	}
	f, err := NewGraded(m, els, g)
	if err != nil {
		t.Fatalf("построение рельефа с правками: %v", err)
	}
	return f
}

// gradedCell — клетка уровня 0 на удалении от оси пути: покрывает
// [cx·256, (cx+1)·256) × [cz·256, (cz+1)·256), ось затравки идёт вдоль y=0, и
// клетка с cz ≥ 1 целиком лежит вне коридора земляных работ (reach 36.5 м) —
// тесты композиции правок не задевают путь.
func gradedCell(cx, cz int, height int16) GradeCell {
	return GradeCell{Level: 0, CX: cx, CZ: cz, HeightCm: height}
}

// TestGradingPlateauIsExactPatchHeight — правка кладёт землю РОВНО на свою
// отметку, не читая рабочую поверхность (инвариант §3): отметка правки
// абсолютная, а не приращение от природы или соседа, и повторное применение
// идемпотентно. Плато — полуоткрытая клетка: правый и верхний край (общие
// отсчёты с соседом) остаются натурой, шов не рвётся.
func TestGradingPlateauIsExactPatchHeight(t *testing.T) {
	m := loadMap(t)
	f := buildGraded(t, m, Grading{Cells: []GradeCell{gradedCell(0, 1, 500)}})
	// Внутренние отсчёты клетки (0, 1): плато ровно 500 см от base_z.
	a := chunk.Address{Level: 0, CX: 0, CZ: 1}
	hs, err := f.ChunkHeights(a)
	if err != nil {
		t.Fatalf("высоты: %v", err)
	}
	for j := range chunk.Samples - 1 {
		for i := range chunk.Samples - 1 {
			if got := hs[chunk.Index(i, j)]; got != 500 {
				t.Fatalf("отсчёт (%d, %d): %d см, ожидалось 500 — плато обязано лежать ровно на отметке правки", i, j, got)
			}
		}
	}
	// Общая граница с соседом — не плато: полуоткрытая клетка отдаёт её
	// соседу, и оба соседа согласованы (шовный инвариант).
	if got := hs[chunk.Index(chunk.Samples-1, 0)]; got == 500 {
		t.Fatalf("правый край клетки (общий с соседом) попал в плато: %d см", got)
	}
	if got := hs[chunk.Index(0, chunk.Samples-1)]; got == 500 {
		t.Fatalf("верхний край клетки (общий с соседом) попал в плато: %d см", got)
	}
	// Соседняя клетка правкой не покрыта — земля природная, и правка туда не
	// протекла: значение в той же точке, что и у поля без правок.
	plain, _ := buildField(t, m)
	nb := chunk.Address{Level: 0, CX: 1, CZ: 1}
	plainHs, err := plain.ChunkHeights(nb)
	if err != nil {
		t.Fatalf("высоты соседа: %v", err)
	}
	gradedHs, err := f.ChunkHeights(nb)
	if err != nil {
		t.Fatalf("высоты соседа (с правкой рядом): %v", err)
	}
	for i := range plainHs {
		if plainHs[i] != gradedHs[i] {
			t.Fatalf("правка протекла в соседнюю клетку: отсчёт %d: %d → %d", i, plainHs[i], gradedHs[i])
		}
	}
}

// TestGradingCompositionOrderIndependent — порядок применения правок не влияет
// на результат (спека §3, следствие 1): каждая правка — функция природной
// поверхности и своего исходника, композиция — множество независимых клеток.
// Два порядка одного множества дают байт в байт одинаковые чанки.
func TestGradingCompositionOrderIndependent(t *testing.T) {
	m := loadMap(t)
	a := []GradeCell{gradedCell(0, 1, 500), gradedCell(1, 1, 700)}
	b := []GradeCell{gradedCell(1, 1, 700), gradedCell(0, 1, 500)}
	fA := buildGraded(t, m, Grading{Cells: a})
	fB := buildGraded(t, m, Grading{Cells: b})
	for _, addr := range []chunk.Address{
		{Level: 0, CX: 0, CZ: 1},
		{Level: 0, CX: 1, CZ: 1},
		{Level: 1, CX: 0, CZ: 0}, // грубый уровень над обеими клетками
	} {
		hA, err := fA.ChunkHeights(addr)
		if err != nil {
			t.Fatalf("высоты A %v: %v", addr, err)
		}
		hB, err := fB.ChunkHeights(addr)
		if err != nil {
			t.Fatalf("высоты B %v: %v", addr, err)
		}
		for i := range hA {
			if hA[i] != hB[i] {
				t.Fatalf("чанк %v: отсчёт %d разошёлся: %d vs %d — порядок правок повлиял на результат",
					addr, i, hA[i], hB[i])
			}
		}
	}
}

// TestGradingSameCellSameHeightComposes — две правки одной клетки с РАВНОЙ
// отметкой — одно множество, не два слоя: композиция идемпотентна, повтор не
// удваивает подъём (спека §7: дельты не аддитивны, отсюда абсолютные отметки).
func TestGradingSameCellSameHeightComposes(t *testing.T) {
	m := loadMap(t)
	g := Grading{Cells: []GradeCell{gradedCell(0, 1, 500), gradedCell(0, 1, 500)}}
	f := buildGraded(t, m, g)
	cm, err := f.HeightCm(128, 384)
	if err != nil {
		t.Fatalf("высота: %v", err)
	}
	if cm != 500 {
		t.Fatalf("повторная правка той же отметки изменила землю: %d см", cm)
	}
}

// TestGradingSameCellDifferentHeightsRefused — две правки одной клетки с
// РАЗНЫМИ отметками — отказ (спека §5: две площадки в общей области —
// одинаковая отметка в пределах допуска, иначе отказ; допуск не объявлен —
// расхождение равно отказу). Отказ, а не «последний победил»: последний
// победил превратил бы инженерный конфликт в зависимость от порядка.
func TestGradingSameCellDifferentHeightsRefused(t *testing.T) {
	m := loadMap(t)
	_, els, err := track.Propagate(m)
	if err != nil {
		t.Fatalf("распространение поз: %v", err)
	}
	_, err = NewGraded(m, els, Grading{Cells: []GradeCell{gradedCell(0, 1, 500), gradedCell(0, 1, 700)}})
	if err == nil {
		t.Fatal("две отметки одной клетки приняты — конфликт жёстких габаритов обязан быть отказом")
	}
}

// TestGradingNonZeroLevelRefused — правка адресуется клеткой уровня 0, и
// только им: клетки разных уровней пересекались бы в плане, оставаясь разными
// ключами, и конфликт «по клетке» был бы пропущен.
func TestGradingNonZeroLevelRefused(t *testing.T) {
	m := loadMap(t)
	_, els, err := track.Propagate(m)
	if err != nil {
		t.Fatalf("распространение поз: %v", err)
	}
	c := GradeCell{Level: 1, CX: 0, CZ: 0, HeightCm: 500}
	if _, err := NewGraded(m, els, Grading{Cells: []GradeCell{c}}); err == nil {
		t.Fatal("правка уровня 1 принята — разрешён только уровень 0")
	}
}

// TestGradingOverPathPlatformRefused — правка, накрывшая ЖЁСТКУЮ площадку пути
// на другой отметке, — два несовместимых жёстких габарита, отказ на компиляции
// (спека §4.2, §4.3: пост-инвариант — поверхность обязана удовлетворять
// габариту КАЖДОГО объекта; компромисс молча подвинул бы проектную отметку).
func TestGradingOverPathPlatformRefused(t *testing.T) {
	m := loadMap(t)
	// Платформа пути: отсчёт прямо на оси. Правка той же клетки (0,0) на
	// заведомо другой отметке обязана отказать на первом же отсчёте платформы.
	f := buildGraded(t, m, Grading{Cells: []GradeCell{gradedCell(0, 0, 500)}})
	if _, err := f.HeightCm(128, 2); err == nil {
		t.Fatal("правка поверх основной площадки пути принята — два несовместимых жёстких габарита обязаны отказать")
	}
}

// TestGradingMatchingPlatformAllowed — правка клетки, несущей путь, на ОТМЕТКЕ
// площадки законна: два жёстких габарита совпали в пределах квантования.
// Поверх откоса правка побеждает (жёсткое вправе перерезать мягкое, §4.1), и
// плато ложится ровно на отметку правки, а не на природную землю.
func TestGradingMatchingPlatformAllowed(t *testing.T) {
	m := loadMap(t)
	plain, _ := buildField(t, m)
	// Отметка площадки в сантиметрах — от незатронутого поля, а не из
	// формулы: тест не знает профиль оси и не должен его угадывать.
	pc, err := plain.HeightCm(128, 2)
	if err != nil {
		t.Fatalf("отметка площадки: %v", err)
	}
	f := buildGraded(t, m, Grading{Cells: []GradeCell{gradedCell(0, 0, pc)}})
	got, err := f.HeightCm(128, 2)
	if err != nil {
		t.Fatalf("правка на отметке площадки отказана: %v", err)
	}
	if got != pc {
		t.Fatalf("платформа: %d см, ожидалось %d", got, pc)
	}
	// Откос (мягкий габарит пути): правка перерезает его — плато ровно на
	// отметке правки, где путь тянул бы землю к природной.
	slope, err := f.HeightCm(128, 20)
	if err != nil {
		t.Fatalf("откос: %v", err)
	}
	if slope != pc {
		t.Fatalf("откос: %d см, ожидалось %d — жёсткое обязано перерезать мягкое", slope, pc)
	}
	plainSlope, err := plain.HeightCm(128, 20)
	if err != nil {
		t.Fatalf("природный откос: %v", err)
	}
	if plainSlope == pc {
		t.Fatalf("тест не различает: природный откос (%d см) совпал с отметкой правки", plainSlope)
	}
}

// TestGradingBounds — замкнутый прямоугольник клетки: габарит для замыкания
// инвалидизации обязан накрыть клетку целиком, включая границы (шовный
// инвариант — сосед по границе входит в пересборку).
func TestGradingBounds(t *testing.T) {
	c := gradedCell(-1, 2, 300)
	minX, minZ, maxX, maxZ := c.Bounds()
	if minX != -256 || maxX != 0 || minZ != 512 || maxZ != 768 {
		t.Fatalf("габарит клетки (-1, 2): x [%v, %v], z [%v, %v] — ожидалось x [-256, 0], z [512, 768]",
			minX, maxX, minZ, maxZ)
	}
	// Отрицательные координаты: floor, а не целочисленное деление — клетка
	// -1 покрывает [-256, 0), и точка -4 обязана попасть в неё.
	f := buildGraded(t, loadMap(t), Grading{Cells: []GradeCell{gradedCell(-1, 2, 300)}})
	cm, err := f.HeightCm(-4, 600)
	if err != nil {
		t.Fatalf("высота в отрицательной клетке: %v", err)
	}
	if cm != 300 {
		t.Fatalf("отрицательная клетка: %d см, ожидалось 300", cm)
	}
	if math.Floor(-4.0/chunk.SideM(0)) != -1 {
		t.Fatal("проверка самого floor: -4/256 обязан дать -1")
	}
}
