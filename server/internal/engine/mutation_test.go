package engine

import (
	"fmt"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// mark — поддельная правка партии.
//
// Доменных правок в боевом коде нет и в этой вехе не будет (см. mutation.go),
// поэтому механизм проверяется этой: она дописывает единицу с известным именем,
// то есть оставляет в партии след, по которому видно И ЧТО правка применилась,
// И В КАКОМ ПОРЯДКЕ относительно других.
type mark struct {
	name string
	fail bool
}

func (m mark) Name() string { return "mark" }

func (m mark) Apply(g *match.Match) error {
	if m.fail {
		return fmt.Errorf("правка %s отказала нарочно", m.name)
	}
	g.Units = append(g.Units, match.Unit{
		ID: m.name, Name: m.name, Type: "VL80",
		At: netloc.PointU{Element: seedmap.StationMain, U: 10, Direction: netloc.DirForward},
	})
	return nil
}

func names(g match.Match) []string {
	out := make([]string, 0, len(g.Units))
	for _, u := range g.Units {
		out = append(out, u.Name)
	}
	return out
}

// TestMutationAppliesAtTickAndNotBefore — граница между сетью и миром.
//
// Правка, поданная между тиками, обязана лежать в очереди: применить её сразу
// значило бы менять партию из горутины транспорта, то есть ровно та гонка,
// ради невозможности которой движок владеет состоянием единолично.
func TestMutationAppliesAtTickAndNotBefore(t *testing.T) {
	e := New(fixture(), nil)
	done := e.Submit(mark{name: "M"})
	if got := names(e.Snapshot().Match); len(got) != 1 {
		t.Fatalf("правка применилась до тика: %v", got)
	}
	select {
	case err := <-done:
		t.Fatalf("исход правки пришёл до тика: %v", err)
	default:
	}
	e.Step()
	if err := <-done; err != nil {
		t.Fatalf("правка отказала: %v", err)
	}
	if got := names(e.Snapshot().Match); len(got) != 2 || got[1] != "M" {
		t.Fatalf("после тика в партии %v", got)
	}
}

// TestMutationsApplyInSubmissionOrder — шов 10a: порядок команд детерминирован.
//
// Проверяется именно ПОРЯДОК ПОСТУПЛЕНИЯ, а не порядок, в котором горутины
// добрались до замка: все три правки поданы до тика и обязаны примениться так,
// как поданы.
func TestMutationsApplyInSubmissionOrder(t *testing.T) {
	e := New(fixture(), nil)
	for _, n := range []string{"A", "B", "C"} {
		e.Submit(mark{name: n})
	}
	e.Step()
	got := names(e.Snapshot().Match)
	want := []string{"LOCO_1", "A", "B", "C"}
	if len(got) != len(want) {
		t.Fatalf("в партии %v, ожидалось %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("порядок применения %v, ожидался %v", got, want)
		}
	}
}

// TestFailedMutationReachesItsSubmitter — отказ правки принадлежит тому, кто её
// подал, и никому больше: сосед по очереди обязан примениться как ни в чём не
// бывало.
func TestFailedMutationReachesItsSubmitter(t *testing.T) {
	e := New(fixture(), nil)
	bad := e.Submit(mark{name: "X", fail: true})
	good := e.Submit(mark{name: "Y"})
	e.Step()
	if err := <-bad; err == nil {
		t.Fatal("отказавшая правка вернула успех")
	}
	if err := <-good; err != nil {
		t.Fatalf("соседняя правка пострадала: %v", err)
	}
	if got := names(e.Snapshot().Match); len(got) != 2 || got[1] != "Y" {
		t.Fatalf("в партии %v, ожидался только след Y", got)
	}
}

// TestQueueIsDrainedWholeEachTick — очередь опустошается целиком, а не по одной
// правке за тик: иначе наплыв команд растил бы очередь быстрее, чем она
// разбирается, и мир отставал бы от игрока молча.
func TestQueueIsDrainedWholeEachTick(t *testing.T) {
	e := New(fixture(), nil)
	const n = 32
	for i := range n {
		e.Submit(mark{name: fmt.Sprintf("M%02d", i)})
	}
	e.Step()
	if got := len(e.Snapshot().Match.Units); got != n+1 {
		t.Fatalf("за один тик применилось %d правок из %d", got-1, n)
	}
}

// TestSubscriberSeesEveryTickAndStopsOnUnsubscribe — подписка на границу тика.
func TestSubscriberSeesEveryTickAndStopsOnUnsubscribe(t *testing.T) {
	e := New(fixture(), nil)
	sub, unsubscribe := e.Subscribe()
	// Первый снимок кладётся при подписке — без него подписчик в неподвижном
	// мире не узнал бы о нём ничего до первого изменения.
	first := <-sub
	if first.Tick != 0 {
		t.Fatalf("первый снимок с тика %d, ожидался нулевой", first.Tick)
	}
	e.Step()
	if got := (<-sub).Tick; got != 1 {
		t.Fatalf("после шага пришёл тик %d", got)
	}
	unsubscribe()
	e.Step()
	select {
	case s := <-sub:
		t.Fatalf("после отписки пришёл снимок тика %d", s.Tick)
	default:
	}
}

// TestSlowSubscriberDropsSnapshotsAndKeepsTicking — мир не ждёт сети.
//
// Подписчик, не забравший прошлый снимок, обязан пропустить его, а не
// задержать тик. Потери он не несёт: снимок полный, и следующий содержит всё,
// что содержал пропущенный, — на этом свойстве и держится право ронять.
func TestSlowSubscriberDropsSnapshotsAndKeepsTicking(t *testing.T) {
	e := New(fixture(), nil)
	sub, unsubscribe := e.Subscribe()
	defer unsubscribe()
	for range 5 {
		e.Step()
	}
	if got := e.Snapshot().Tick; got != 5 {
		t.Fatalf("мир остановился на тике %d — тик ждал подписчика", got)
	}
	// В буфере лежит ПОСЛЕДНИЙ снимок, а не первый: устаревшие вытеснены.
	// Обратный выбор показывал бы отставшему подписчику прошлое как настоящее.
	if got := (<-sub).Tick; got != 5 {
		t.Fatalf("в буфере тик %d, ожидался последний (5)", got)
	}
	e.Step()
	if got := (<-sub).Tick; got != 6 {
		t.Fatalf("после освобождения буфера пришёл тик %d, ожидался 6", got)
	}
}
