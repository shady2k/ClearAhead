package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// loco1ID — идентификатор локомотива фикстуры: UUIDv7 боевой расстановки
// (maps/st_a_placement.json, метка LOCO_1). Движок тождество не проверяет —
// константа держит фикстуру согласованной с остальными тестами.
const loco1ID = "01a3185c-6001-7242-8242-000000424242"

func fixture() *match.Match {
	return &match.Match{ID: "M1", Region: "ST_A", Units: []match.Unit{{
		ID: loco1ID, Name: "LOCO_1", Type: "VL80",
		At: netloc.PointU{Element: seedmap.StationMain, U: 150, Direction: netloc.DirForward},
	}}}
}

// TestStepAdvancesTimeByExactlyOneTick — восемь часов модельного времени за долю
// секунды реального. Проверяется не скорость, а РАВЕНСТВО: время партии есть
// номер тика, умноженный на длительность, и ничто другое.
func TestStepAdvancesTimeByExactlyOneTick(t *testing.T) {
	e := New(fixture(), nil)
	if s := e.Snapshot(); s.Tick != 0 || s.Time != 0 {
		t.Fatalf("новая партия начинает с тика %d, времени %s", s.Tick, s.Time)
	}
	const want = 8 * units.Hour
	steps := int(want / TickDuration)
	for range steps {
		e.Step()
	}
	s := e.Snapshot()
	if s.Time != want {
		t.Fatalf("после %d шагов время %s, ожидалось %s", steps, s.Time, want)
	}
	if s.Tick != Tick(steps) {
		t.Fatalf("тик %d, ожидался %d", s.Tick, steps)
	}
}

// TestModelTimeDoesNotFollowWallClock — главный инвариант пакета: настенные часы
// решают, КОГДА позвать Step, и не решают, НА СКОЛЬКО продвинулось время мира.
//
// Тест намеренно спит между шагами. Если время когда-нибудь начнут считать
// разницей показаний часов, он покажет это первым: три шага обязаны дать ровно
// три тика, сколько бы реального времени между ними ни прошло.
func TestModelTimeDoesNotFollowWallClock(t *testing.T) {
	e := New(fixture(), nil)
	for range 3 {
		e.Step()
		time.Sleep(25 * time.Millisecond)
	}
	if s := e.Snapshot(); s.Time != 3*TickDuration {
		t.Fatalf("время %s, ожидалось %s: модельное время не выводится из настенного",
			s.Time, 3*TickDuration)
	}
}

// TestPlanKeepsRemainder — накопитель отдаёт целые тики и хранит остаток.
// Потеря остатка — систематический дрейф: при периоде таймера, не кратном тику,
// мир отставал бы на каждом пробуждении.
func TestPlanKeepsRemainder(t *testing.T) {
	cases := []struct {
		name    string
		acc     units.SimTime
		steps   int
		rest    units.SimTime
		dropped units.SimTime
	}{
		{"пусто", 0, 0, 0, 0},
		{"меньше тика", 40 * units.Millisecond, 0, 40 * units.Millisecond, 0},
		{"ровно тик", TickDuration, 1, 0, 0},
		{"два с половиной", 250 * units.Millisecond, 2, 50 * units.Millisecond, 0},
		{"ровно потолок", MaxCatchUp, 10, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			steps, rest, dropped := plan(c.acc)
			if steps != c.steps || rest != c.rest || dropped != c.dropped {
				t.Fatalf("plan(%s) = %d шагов, остаток %s, потеряно %s; ожидалось %d, %s, %s",
					c.acc, steps, rest, dropped, c.steps, c.rest, c.dropped)
			}
		})
	}
}

// TestPlanDropsBeyondCatchUpWithoutSkippingTicks — крышка ноутбука.
//
// Проверяются ОБА свойства разом, потому что порознь они ничего не значат:
// потерянное время названо числом (иначе мир прыгнул бы молча) И отданные тики
// остались целыми (иначе движок пропустил бы шаги физики, а не отстал).
func TestPlanDropsBeyondCatchUpWithoutSkippingTicks(t *testing.T) {
	const slept = units.Hour
	steps, rest, dropped := plan(slept)
	if steps != int(MaxCatchUp/TickDuration) {
		t.Fatalf("шагов %d, ожидалось %d — за одно пробуждение отдаётся не больше потолка",
			steps, MaxCatchUp/TickDuration)
	}
	if rest != 0 {
		t.Fatalf("остаток %s, ожидался ноль: потолок кратен тику", rest)
	}
	if dropped != slept-MaxCatchUp {
		t.Fatalf("потеряно %s, ожидалось %s", dropped, slept-MaxCatchUp)
	}
	// Потерянное плюс отданное равно тому, что накопилось: время не исчезает
	// незаметно и не возникает из ниоткуда — оно либо прожито, либо объявлено
	// потерянным.
	if got := units.SimTime(steps)*TickDuration + rest + dropped; got != slept {
		t.Fatalf("отдано плюс остаток плюс потеряно = %s, накопилось %s", got, slept)
	}
}

// TestSnapshotIsCopyNotView — снимок обязан быть копией: читатель, правящий
// возвращённый срез, иначе правил бы партию.
func TestSnapshotIsCopyNotView(t *testing.T) {
	e := New(fixture(), nil)
	s := e.Snapshot()
	s.Match.Units[0].ID = "ПОРЧА"
	s.Match.Region = "ST_B"
	again := e.Snapshot()
	if again.Match.Units[0].ID != loco1ID {
		t.Fatalf("единица в движке стала %q — снимок оказался видом, а не копией",
			again.Match.Units[0].ID)
	}
	if again.Match.Region != "ST_A" {
		t.Fatalf("регион в движке стал %q", again.Match.Region)
	}
}

// TestRunAdvancesTimeWhileSnapshotsAreRead — петля крутится, читатели читают.
//
// Тест написан ради -race: он ставит писателя и читателей в те же отношения, в
// каких они стоят на боевом сервере (движок двигает время, обработчики HTTP
// спрашивают снимок), и без замка детектор гонок ловит это здесь.
//
// Про число тиков утверждается только «хотя бы один»: сколько именно их успеет
// пройти — свойство загруженности машины, а не движка, и точное число сделало
// бы тест мигающим.
func TestRunAdvancesTimeWhileSnapshotsAreRead(t *testing.T) {
	e := New(fixture(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		e.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			s := e.Snapshot()
			if s.Time != s.Tick.Time() {
				t.Errorf("снимок несогласован: тик %d, время %s", s.Tick, s.Time)
				return
			}
		}
	}()
	time.Sleep(350 * time.Millisecond)
	cancel()
	wg.Wait()
	if s := e.Snapshot(); s.Tick < 1 {
		t.Fatalf("за 350 мс настенного времени тиков %d — петля не крутится", s.Tick)
	}
}

// TestRunReturnsOnContextCancel — срок жизни петли принадлежит тому, кто её
// запустил. Петля, не возвращающаяся по отмене, — это утёкшая горутина, и
// увидят её не здесь, а на выходе сервера.
func TestRunReturnsOnContextCancel(t *testing.T) {
	e := New(fixture(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run не вернулся через две секунды после отмены контекста")
	}
}
