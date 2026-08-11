package chunk

import "testing"

// Инварианты сетки. Именно они были нарушены в согласованном «256 м, шаг 5 м,
// 53×53», и проверка стоит здесь затем, чтобы та же арифметика не повторилась
// при следующей правке чисел.
func TestИнвариантыСетки(t *testing.T) {
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
func TestОтсчётовПоровнуНаВсехУровнях(t *testing.T) {
	for level := range 8 {
		if got := int(SideM(level)/StepM(level)) + 1; got != Samples {
			t.Fatalf("уровень %d: отсчётов %d, ожидалось %d", level, got, Samples)
		}
	}
}

// Каждый следующий уровень вдвое шире и вдвое грубее.
func TestУровеньВдвоеШиреИГрубее(t *testing.T) {
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
func TestОбщийРядСовпадаетССоседом(t *testing.T) {
	a := Address{Region: "KRD", Level: 0, CX: 3, CZ: -2}
	справа := Address{Region: "KRD", Level: 0, CX: 4, CZ: -2}
	снизу := Address{Region: "KRD", Level: 0, CX: 3, CZ: -1}

	for j := range Samples {
		ax, az := a.SampleM(Samples-1, j)
		bx, bz := справа.SampleM(0, j)
		if ax != bx || az != bz {
			t.Fatalf("столбец %d: (%v, %v) против (%v, %v)", j, ax, az, bx, bz)
		}
	}
	for i := range Samples {
		ax, az := a.SampleM(i, Samples-1)
		bx, bz := снизу.SampleM(i, 0)
		if ax != bx || az != bz {
			t.Fatalf("ряд %d: (%v, %v) против (%v, %v)", i, ax, az, bx, bz)
		}
	}
}

// Чанк покрывает ровно свою сторону: от угла до угла следующего.
func TestЧанкПокрываетСвоюСторону(t *testing.T) {
	a := Address{Level: 2, CX: 5, CZ: 7}
	ox, oz := a.OriginM()
	fx, fz := a.SampleM(Samples-1, Samples-1)
	if fx-ox != SideM(2) || fz-oz != SideM(2) {
		t.Fatalf("чанк покрывает %v x %v, сторона уровня %v", fx-ox, fz-oz, SideM(2))
	}
}
