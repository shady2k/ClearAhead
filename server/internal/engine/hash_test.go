package engine

import (
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/brake"
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
	e := New(two(), nil)
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

// TestStateHashNoticesPneumatics — ХЕШ ЗАМЕЧАЕТ РАБОТУ ПНЕВМАТИКИ У СТОЯЩЕЙ
// МАШИНЫ.
//
// Проверка заведена по жалобе владельца 2026-08-15: «дёргаются именно стрелки
// манометров; поначалу всё плавно, а потом они начинают обновляться раз в
// секунду». Поначалу плавно — потому что машина ехала и хеш менялся положением;
// раз в секунду — как только она встала, а давления продолжали меняться: их в
// хеше не было, рассылка идёт ПО СМЕНЕ ХЕША, и снапшоты сводились к секундному
// биению.
//
// Это ТРЕТИЙ случай одного рода: до давлений так же забывали органы управления и
// состояние физики, и обе прошлые потери записаны в комментариях у самого хеша.
func TestStateHashNoticesPneumatics(t *testing.T) {
	before := two()
	before.Air = map[string]brake.State{"a": {Main: 9_000_000, Pipe: 5_400_000, Cylinder: 0}}
	after := two()
	after.Air = map[string]brake.State{"a": {Main: 9_000_000, Pipe: 5_400_000, Cylinder: 1_000}}
	if StateHash(before) == StateHash(after) {
		t.Fatal("наполнение цилиндра на тысячную кгс/см² не изменило хеш — снапшот не уйдёт")
	}
	// И магистраль, и резервуары — каждое порознь: пропущенное поле не заметит
	// именно своих изменений, а общая проверка «хоть что-то менялось» это скрыла
	// бы.
	pipe := two()
	pipe.Air = map[string]brake.State{"a": {Main: 9_000_000, Pipe: 5_399_000, Cylinder: 0}}
	if StateHash(before) == StateHash(pipe) {
		t.Fatal("разрядка магистрали не изменила хеш")
	}
	main := two()
	main.Air = map[string]brake.State{"a": {Main: 8_999_000, Pipe: 5_400_000, Cylinder: 0}}
	if StateHash(before) == StateHash(main) {
		t.Fatal("расход главных резервуаров не изменил хеш")
	}
}

// TestStateHashNoticesController — позиция контроллера ползёт к рукоятке и на
// стоянке: поставил рукоятку, машина ещё не тронулась, а позиция уже идёт. Без
// неё в хеше ползунок тяги на пульте шагал бы раз в секунду.
func TestStateHashNoticesController(t *testing.T) {
	before := two()
	before.Notches = map[string]int{"a": 0}
	after := two()
	after.Notches = map[string]int{"a": 20}
	if StateHash(before) == StateHash(after) {
		t.Fatal("продвижение контроллера на две сотых позиции не изменило хеш")
	}
}
