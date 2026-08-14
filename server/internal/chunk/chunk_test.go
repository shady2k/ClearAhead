package chunk

import "testing"

// Инварианты сетки. Именно они были нарушены в согласованном «256 м, шаг 5 м,
// 53×53», и проверка стоит здесь затем, чтобы та же арифметика не повторилась
// при следующей правке чисел.
func TestGridInvariants(t *testing.T) {
	if SideM0%StepM0 != 0 {
		t.Fatalf("шаг %d не делит сторону %d нацело — сетка невыразима целым числом отсчётов",
			StepM0, SideM0)
	}
	if got, want := (Samples-1)*StepM0, SideM0; got != want {
		t.Fatalf("интервалы покрывают %d м, сторона %d м", got, want)
	}
	if SideM0&(SideM0-1) != 0 {
		t.Fatalf("сторона %d не степень двойки — квадродерево не поделит её пополам", SideM0)
	}
	if SideM0%GrassChunkM != 0 {
		t.Fatalf("чанк травы %d м не ложится в сторону %d м без остатка", GrassChunkM, SideM0)
	}
}

// Число отсчётов постоянно на всех уровнях: от этого зависит то, что размер
// блоба не зависит от уровня и клиент выделяет буфер один раз.
func TestSampleCountIsEqualOnAllLevels(t *testing.T) {
	for level := range 8 {
		if got := int(SideM(level)/StepM(level)) + 1; got != Samples {
			t.Fatalf("уровень %d: отсчётов %d, ожидалось %d", level, got, Samples)
		}
	}
}

// Каждый следующий уровень вдвое шире и вдвое грубее.
func TestLevelIsTwiceWiderAndCoarser(t *testing.T) {
	for level := range 7 {
		if SideM(level+1) != 2*SideM(level) {
			t.Fatalf("уровень %d: сторона не удвоилась", level)
		}
		if StepM(level+1) != 2*StepM(level) {
			t.Fatalf("уровень %d: шаг не удвоился", level)
		}
	}
}

// ОБЩИЙ РЯД. Последний отсчёт чанка обязан совпасть с нулевым отсчётом соседа —
// не приблизительно, а той же самой точкой. Без этого на стыке остаётся щель.
//
// Проверяется координатами, а не значениями рельефа: совпадение обеспечивается
// тем, что у общего узла один аргумент, и именно это здесь и утверждается.
func TestSharedRowMatchesNeighbor(t *testing.T) {
	a := Address{Region: "KRD", Level: 0, CX: 3, CZ: -2}
	right := Address{Region: "KRD", Level: 0, CX: 4, CZ: -2}
	below := Address{Region: "KRD", Level: 0, CX: 3, CZ: -1}

	for j := range Samples {
		ax, az := a.SampleM(Samples-1, j)
		bx, bz := right.SampleM(0, j)
		if ax != bx || az != bz {
			t.Fatalf("столбец %d: (%v, %v) против (%v, %v)", j, ax, az, bx, bz)
		}
	}
	for i := range Samples {
		ax, az := a.SampleM(i, Samples-1)
		bx, bz := below.SampleM(i, 0)
		if ax != bx || az != bz {
			t.Fatalf("ряд %d: (%v, %v) против (%v, %v)", i, ax, az, bx, bz)
		}
	}
}

// Чанк покрывает ровно свою сторону: от угла до угла следующего.
func TestChunkCoversItsSide(t *testing.T) {
	a := Address{Level: 2, CX: 5, CZ: 7}
	ox, oz := a.OriginM()
	fx, fz := a.SampleM(Samples-1, Samples-1)
	if fx-ox != SideM(2) || fz-oz != SideM(2) {
		t.Fatalf("чанк покрывает %v x %v, сторона уровня %v", fx-ox, fz-oz, SideM(2))
	}
}

// Порядок байт назван в контракте явно — значит должен проверяться, а не
// подразумеваться. Автор адаптера читает блоб по этому правилу.
func TestByteOrderIsLittleEndian(t *testing.T) {
	h := make([]int16, Samples*Samples)
	h[0] = 0x0102
	h[1] = -2 // 0xFFFE
	b, err := EncodeHeights(h)
	if err != nil {
		t.Fatalf("кодирование: %v", err)
	}
	if b[0] != 0x02 || b[1] != 0x01 {
		t.Fatalf("первый отсчёт лёг как %#x %#x, ожидалось 02 01 (младший байт первым)", b[0], b[1])
	}
	if b[2] != 0xFE || b[3] != 0xFF {
		t.Fatalf("отрицательный отсчёт лёг как %#x %#x, ожидалось FE FF", b[2], b[3])
	}
	if len(b) != HeightsBytes {
		t.Fatalf("блоб %d байт, объявлено %d", len(b), HeightsBytes)
	}
}

// Порядок обхода — строками: индекс (i, j) обязан совпасть с j*Samples+i,
// иначе клиент прочтёт поверхность транспонированной.
func TestTraversalOrderIsRowMajor(t *testing.T) {
	if Index(0, 1) != Samples {
		t.Fatalf("Index(0,1) = %d, ожидалось %d — обход должен идти строками", Index(0, 1), Samples)
	}
	if Index(1, 0) != 1 {
		t.Fatalf("Index(1,0) = %d, ожидалось 1", Index(1, 0))
	}
}

// Блоб неверного размера — отказ, а не молчаливое усечение.
func TestBlobOfWrongSizeIsRejected(t *testing.T) {
	if _, err := DecodeHeights(make([]byte, HeightsBytes-1)); err == nil {
		t.Fatal("короткий блоб принят")
	}
	if _, err := EncodeHeights(make([]int16, 3)); err == nil {
		t.Fatal("неполный набор отсчётов принят")
	}
}

// Бит занятости ставится и снимается, и только в пределах блоба.
func TestForestBitSetAndClear(t *testing.T) {
	blob := make([]byte, ForestBytes)
	SetForestOccupied(blob, 3, 7)
	if !ForestOccupied(blob, 3, 7) {
		t.Fatal("бит не встал")
	}
	ClearForestOccupied(blob, 3, 7)
	if ForestOccupied(blob, 3, 7) {
		t.Fatal("бит не снялся")
	}
	// Соседние биты того же байта не задеты.
	if blob[CoverIndex(3, 7)/8] != 0 {
		t.Fatalf("байт изменился целиком: %#x", blob[CoverIndex(3, 7)/8])
	}
}

// Вне сетки операции молчат: блоб остаётся прежним, ошибки нет.
//
// Молчание здесь — не подмена, а граница API: проверка диапазона ячейки — дело
// валидатора исходника (vegetation), а не битовой операции, которую зовут с
// уже проверенными координатами.
func TestForestBitOutOfRangeIsNoop(t *testing.T) {
	blob := make([]byte, ForestBytes)
	SetForestOccupied(blob, CoverCells, 0)
	SetForestOccupied(blob, 0, -1)
	ClearForestOccupied(blob, CoverCells, 0)
	for _, b := range blob {
		if b != 0 {
			t.Fatalf("блоб изменился: %#x", b)
		}
	}
}

// ForestJitter — КОНТРАКТ с клиентом, а не деталь сервера: смещение внутри
// ячейки, высота ствола и поворот считаются от адреса одинаково на сервере и
// клиенте (шапка функции). Числа ниже — замер текущей реализации, и правка
// функции без правки теста означает разъезд двух рендереров.
//
// Сверка побитовая, без допуска: функция целочисленная, деление на 2^32 точно,
// и любой другой результат — другой адрес.
func TestForestJitterIsContract(t *testing.T) {
	cases := []struct {
		cx, cz, i, j int
		dx, dz, h    float64
	}{
		{0, 0, 0, 0, 0.99702195799909532, 0.97566940705291927, 0.30519589595496655},
		{1, -2, 5, 7, 0.22053048037923872, 0.60869828867726028, 0.30867270193994045},
		{-3, 4, 63, 63, 0.28323475806973875, 0.054823124082759023, 0.85280528129078448},
		{12, 34, 17, 3, 0.60489303478971124, 0.10970722115598619, 0.73523029452189803},
	}
	for _, c := range cases {
		dx, dz, h := ForestJitter(c.cx, c.cz, c.i, c.j)
		if dx != c.dx || dz != c.dz || h != c.h {
			t.Fatalf("ForestJitter(%d, %d, %d, %d) = (%.17g, %.17g, %.17g), ожидалось (%.17g, %.17g, %.17g)",
				c.cx, c.cz, c.i, c.j, dx, dz, h, c.dx, c.dz, c.h)
		}
	}
}
