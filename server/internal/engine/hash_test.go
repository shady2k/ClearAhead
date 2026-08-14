package engine

import (
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

func two() *match.Match {
	return &match.Match{ID: "M1", Region: "ST_A", Units: []match.Unit{
		{ID: "a", Name: "A", Type: "VL80",
			At: netloc.PointU{Element: seedmap.StationMain, U: 150, Direction: netloc.DirForward}},
		{ID: "b", Name: "B", Type: "VL80",
			At: netloc.PointU{Element: seedmap.StationMain, U: 250, Direction: netloc.DirReverse}},
	}}
}

// TestStateHashIgnoresUnitOrder — порядок в срезе есть порядок строк в файле
// расстановки, то есть свойство ДОКУМЕНТА. Переставленные строки описывают тот
// же мир, и хеш обязан это подтверждать: иначе канал слал бы снапшот на
// перестановку, которой никто не делал.
func TestStateHashIgnoresUnitOrder(t *testing.T) {
	direct := two()
	swapped := two()
	swapped.Units[0], swapped.Units[1] = swapped.Units[1], swapped.Units[0]
	if StateHash(direct) != StateHash(swapped) {
		t.Fatal("перестановка единиц изменила хеш состояния")
	}
}

// TestStateHashNoticesMovement — обратная сторона: сдвиг на миллиметр обязан
// менять хеш, иначе рассылка по изменению не заметит движения.
func TestStateHashNoticesMovement(t *testing.T) {
	before := two()
	after := two()
	after.Units[0].At.U += 0.001
	if StateHash(before) == StateHash(after) {
		t.Fatal("сдвиг на миллиметр не изменил хеш")
	}
}

// TestStateHashQuantizesBelowMicrometer — правило проекта «float64 не
// сверяется байт в байт» здесь исполняется, а не декларируется.
//
// Хеш от битов float64 — это сверка байт в байт по определению, и проект уже
// потерял на такой сверке эталон контракта (коммит 4b4d6a7, расхождение ~1e-13
// на неизменном коде при смене машины). Поэтому смещение квантуется в
// микрометры: два состояния, различающиеся меньше чем на микрометр, дают один
// хеш. Разница 1e-9 м = нанометр — ровно тот порядок, в котором расходятся
// вычисления на разных архитектурах.
func TestStateHashQuantizesBelowMicrometer(t *testing.T) {
	before := two()
	after := two()
	after.Units[0].At.U += 1e-9
	if StateHash(before) != StateHash(after) {
		t.Fatal("расхождение в нанометр изменило хеш — сверка не переживёт смены машины")
	}
}

// TestStateHashSeparatesFieldsByLength — соседние поля не должны склеиваться.
//
// Запись без префикса длины дала бы один хеш для ("ab","c") и ("a","bc"): в
// потоке байт это одно и то же. Разные миры с одинаковым хешем — это молчащая
// рассылка, то есть замерший экран у игрока.
func TestStateHashSeparatesFieldsByLength(t *testing.T) {
	left := &match.Match{ID: "ab", Region: "c"}
	right := &match.Match{ID: "a", Region: "bc"}
	if StateHash(left) == StateHash(right) {
		t.Fatal("склейка соседних строк даёт одинаковый хеш")
	}
}

// TestEngineSealsHashEveryTick — хеш живёт в снимке и считается движком, а не
// читателем: второе место, где объявлен канонический порядок полей, разошлось
// бы с первым.
func TestEngineSealsHashEveryTick(t *testing.T) {
	e := New(two())
	start := e.Snapshot()
	if start.Hash == "" {
		t.Fatal("у нового движка нет хеша состояния")
	}
	e.Step()
	if got := e.Snapshot().Hash; got != start.Hash {
		t.Fatalf("пустой тик изменил хеш: %s -> %s", start.Hash, got)
	}
	e.Submit(mark{name: "M"})
	e.Step()
	if got := e.Snapshot().Hash; got == start.Hash {
		t.Fatal("правка партии не изменила хеш")
	}
}
