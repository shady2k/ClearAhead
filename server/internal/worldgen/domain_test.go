package worldgen

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// Домен региона: существование мира решает ПРЯМОУГОЛЬНИК ДОМЕНА, а не габарит
// оси (решение владельца 2026-08-13, спека §4). Эти тесты держат отвязку:
// мир без единого метра пути существует и отдаёт чанки, за доменом — законная
// пустота, а не отказ и не земля.

// TestBootstrapRegistersRegionWithoutPreheating — приёмочный критерий задачи:
// бутстрап заводит запись региона (домен, правило, происхождение), а чанков НЕ
// порождает. Порождение по требованию — основной путь, прогрев — отдельный
// явный шаг, и «бутстрап не греет» обязан быть виден в базе, а не в отчёте.
func TestBootstrapRegistersRegionWithoutPreheating(t *testing.T) {
	s := emptyStore(t)
	m := seedmap.Station(seedmap.WithTerrain())
	rep, seeded, err := Bootstrap(s, m, 1, `{"source":"seedmap","kind":"fixture"}`)
	if err != nil {
		t.Fatalf("бутстрап: %v", err)
	}
	if !seeded {
		t.Fatal("бутстрап не завёл регион на пустой базе")
	}
	if rep.TotalChunks != 0 {
		t.Fatalf("бутстрап породил %d чанков — прогрев не должен подразумеваться", rep.TotalChunks)
	}
	for l := 0; l <= chunk.MaxLevelLimit; l++ {
		if n, err := s.CountChunks(m.MapID, l); err != nil {
			t.Fatal(err)
		} else if n != 0 {
			t.Fatalf("уровень %d: %d чанков после бутстрапа — прогрев живёт внутри бутстрапа", l, n)
		}
	}
	// Регион заведён и несёт домен: манифесту есть что ответить на вопрос «где
	// мир кончается».
	reg, ok, err := s.GetRegion(m.MapID)
	if err != nil || !ok {
		t.Fatalf("регион не заведён: ok=%v err=%v", ok, err)
	}
	if reg.Domain != m.Terrain.Domain {
		t.Fatalf("домен региона %+v, у карты %+v", reg.Domain, m.Terrain.Domain)
	}
}

// TestOnDemandStopsAtDomainEdge — граница мира — ДОМЕН, а не расстояние до оси.
//
// Порождение по требованию не отменяет границы: прямоугольник домена объявляет,
// докуда мир существует ВООБЩЕ. За ним — пустота, а не бесконечная земля, и
// это разные ответы: пустота законна, а земля, которой карта не обещала, —
// враньё, которое клиент не сможет отличить от настоящей.
func TestOnDemandStopsAtDomainEdge(t *testing.T) {
	store, lazy := newLazy(t)
	d := lazy.sel.domain

	// Вдвое дальше восточной границы домена — заведомо снаружи при любом
	// округлении.
	a := chunk.Address{Region: "ST_A", Level: 0, CX: int(2*d.MaxX/chunk.SideM(0)) + 1}
	if lazy.sel.inExtent(a) {
		t.Fatalf("%v: клетка за доменом посчитана существующей", a)
	}
	c, ok, err := lazy.MakeChunk(a, 1)
	if err != nil {
		t.Fatalf("%v: за границей мира получен отказ, а ожидалась пустота: %v", a, err)
	}
	if ok {
		t.Fatalf("%v: за границей домена (x до %v) посчитана земля", a, d.MaxX)
	}
	if c.Hash != "" {
		t.Fatalf("%v: пустота приехала с содержимым", a)
	}
	// И в базу ничего не легло: иначе край мира зарастал бы кэшем.
	if _, ok, err := store.GetChunk(a, 1); err != nil {
		t.Fatalf("чтение: %v", err)
	} else if ok {
		t.Fatalf("%v: за границей домена чанк записан в базу", a)
	}
}

// TestLevel0InDomainCornerFarFromAxisIsComputed — существование решает домен, а
// не ось: уровень 0 в УГЛУ домена, за много километров от любой оси, считается.
//
// Это и есть отвязка из брифа: прогрев кладёт уровень 0 только вблизи пути
// (covers), а порождение по требованию вправе посчитать его всюду, где домен
// объявляет мир. Угол выбран нарочно — клетка, лежащая вне старого круга
// охвата, была бы пустотой, держись мир за ось.
func TestLevel0InDomainCornerFarFromAxisIsComputed(t *testing.T) {
	store, lazy := newLazy(t)
	d := lazy.sel.domain

	// Юго-западный угол домена: от ближайшей точки оси (0, -16) до клетки
	// порядка 14 км — заведомо вне R_0 (512 м), и прогрев её не кладёт.
	side := chunk.SideM(0)
	a := chunk.Address{Region: "ST_A", Level: 0,
		CX: int(math.Floor(d.MinX / side)),
		CZ: int(math.Floor(d.MinZ / side)),
	}
	if !lazy.sel.inExtent(a) {
		t.Fatalf("%v: клетка угла домена объявлена несуществующей", a)
	}
	if lazy.sel.covers(a) {
		t.Fatalf("%v: прогрев кладёт уровень 0 за 14 км от оси — проверять нечего", a)
	}

	got, ok, err := lazy.MakeChunk(a, 1)
	if err != nil {
		t.Fatalf("счёт угла домена: %v", err)
	}
	if !ok {
		t.Fatalf("%v: уровень 0 внутри домена объявлен пустотой", a)
	}
	cached, ok, err := store.GetChunk(a, 1)
	if err != nil || !ok {
		t.Fatalf("угол домена: посчитано, но в базу не легло: ok=%v err=%v", ok, err)
	}
	requireSameChunk(t, "угол домена", cached, got)
}

// TestFullyCarriedTrackStillServesChunks — мир без ТОЧЕК ОСИ существует.
//
// Весь путь несомый (мост через всё): terrain.sampleAxis не даёт ни одной
// точки, Bounds() пуст, а мир обязан существовать — домен решает, ось нет.
// Это полевая половина отвязки; карта с буквально пустой сетью — вопрос к
// валидатору, и он закрыт отдельным решением владельца.
func TestFullyCarriedTrackStillServesChunks(t *testing.T) {
	m := seedmap.Line(
		seedmap.WithID("BRIDGE"),
		seedmap.WithTerrain(),
		seedmap.WithCarryingStructure("bridge", "MOST", seedmap.LineEdgeID, 0, seedmap.LineLengthM),
	)
	s := emptyStore(t)
	if _, _, err := Bootstrap(s, m, 1, `{"source":"seedmap","kind":"fixture"}`); err != nil {
		t.Fatalf("бутстрап: %v", err)
	}
	lazy, err := NewLazy(s, m, m.MapID, 1)
	if err != nil {
		t.Fatalf("порождение по требованию: %v", err)
	}
	if lazy.sel.hasAxis {
		t.Fatal("у полностью несомого пути есть точки оси — проверять нечего")
	}

	// Прогрев пуст: обходить нечего, и это законно, а не отказ.
	if _, err := Generate(s, m, m.MapID, 1, 1); err != nil {
		t.Fatalf("прогрев: %v", err)
	}
	for l := 0; l <= chunk.MaxLevelLimit; l++ {
		if n, err := s.CountChunks(m.MapID, l); err != nil {
			t.Fatal(err)
		} else if n != 0 {
			t.Fatalf("уровень %d: %d чанков при пустой оси", l, n)
		}
	}

	// Порождение по требованию считает клетку внутри домена — земля есть.
	a := chunk.Address{Region: "BRIDGE", Level: 0, CX: 0, CZ: 0}
	if !lazy.sel.inExtent(a) {
		t.Fatalf("%v: клетка внутри домена объявлена несуществующей", a)
	}
	got, ok, err := lazy.MakeChunk(a, 1)
	if err != nil || !ok {
		t.Fatalf("счёт: ok=%v, err=%v", ok, err)
	}
	cached, ok, err := s.GetChunk(a, 1)
	if err != nil || !ok {
		t.Fatalf("посчитано, но в базу не легло: ok=%v err=%v", ok, err)
	}
	requireSameChunk(t, "мост", cached, got)
}

// TestNatureOnlyMapServesChunks — приёмочный критерий sqym.2 целиком: карта БЕЗ
// ЕДИНОГО элемента пути заводится (бутстрап), а порождение по требованию
// считает чанк ВНУТРИ домена. Это сквозной тест на пустую ось: terrain.New
// обязан принять карту без точек оси, Bounds — вернуть пустой габарит, а
// WorkedM на клетке чанка — природную поверхность, а не панику по пустому
// индексу. Младший брат TestFullyCarriedTrackStillServesChunks: там ось пуста
// при непустой СЕТИ (весь путь несомый), здесь сеть пуста целиком — это разные
// половины отвязки, и обе обязаны держаться.
func TestNatureOnlyMapServesChunks(t *testing.T) {
	m := natureOnlyMap()
	s := emptyStore(t)
	if _, _, err := Bootstrap(s, m, 1, `{"source":"seedmap","kind":"fixture"}`); err != nil {
		t.Fatalf("бутстрап: %v", err)
	}
	lazy, err := NewLazy(s, m, m.MapID, 1)
	if err != nil {
		t.Fatalf("порождение по требованию: %v", err)
	}
	if lazy.sel.hasAxis {
		t.Fatal("у карты без элементов есть точки оси — проверять нечего")
	}

	// Прогрев пуст и законен: обходить по габариту оси нечего.
	if _, err := Generate(s, m, m.MapID, 1, 1); err != nil {
		t.Fatalf("прогрев: %v", err)
	}
	for l := 0; l <= chunk.MaxLevelLimit; l++ {
		if n, err := s.CountChunks(m.MapID, l); err != nil {
			t.Fatal(err)
		} else if n != 0 {
			t.Fatalf("уровень %d: %d чанков при пустой сети", l, n)
		}
	}

	// Порождение по требованию считает клетку внутри домена: земля есть.
	a := chunk.Address{Region: m.MapID, Level: 0, CX: 0, CZ: 0}
	if !lazy.sel.inExtent(a) {
		t.Fatalf("%v: клетка внутри домена объявлена несуществующей", a)
	}
	got, ok, err := lazy.MakeChunk(a, 1)
	if err != nil || !ok {
		t.Fatalf("счёт: ok=%v, err=%v", ok, err)
	}
	cached, ok, err := s.GetChunk(a, 1)
	if err != nil || !ok {
		t.Fatalf("посчитано, но в базу не легло: ok=%v err=%v", ok, err)
	}
	requireSameChunk(t, "природа", cached, got)
}

// natureOnlyMap — карта без единого элемента пути: рельеф и домен есть, сети
// нет. Рёбра, геометрия, якоря и решётка убираются целиком; узлы остаются,
// потому что висящие порты с назначением законны, а карта без них отличается
// от законной только формой топологии, не предметом теста.
func natureOnlyMap() *mapfmt.Map {
	return seedmap.Line(
		seedmap.WithID("NATURE"),
		seedmap.WithTerrain(),
		seedmap.Mutate(func(m *mapfmt.Map) {
			m.Anchors = nil
			m.Topology.Edges = nil
			m.Geometry = mapfmt.Geometry{}
			m.Construction = nil
		}),
	)
}

// TestLimiterRefusesWhenQueueFull — переполнение очереди порождения — ОТКАЗ, а
// не зависание (требование брифа, оплаченное замером 39 мс на первый запрос
// вдали от оси).
//
// Каналы заполняются напрямую — это то же состояние, в котором limiter
// оказывается под залпом запросов камеры, только без сотни горутин. Сам select
// с таймаутом и есть доказательство «отказ, а не ожидание».
func TestLimiterRefusesWhenQueueFull(t *testing.T) {
	l := newLimiter()
	for i := 0; i < computeSlots; i++ {
		l.compute <- struct{}{}
	}
	for i := 0; i < queueSlots; i++ {
		l.queue <- struct{}{}
	}

	done := make(chan error, 1)
	go func() { done <- l.acquire() }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrQueueFull) {
			t.Fatalf("переполнение отвечено %v, ожидался ErrQueueFull", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("переполненная очередь не отказала — запрос повис")
	}

	// Разгрузка: освободили счётный слот и место в очереди — и вход снова
	// проходит. Иначе очередь была бы вечным отказом, а не очередью.
	<-l.compute
	<-l.queue
	if err := l.acquire(); err != nil {
		t.Fatalf("после разгрузки очереди вход отказан: %v", err)
	}
}

// TestQueueOverflowRefusesThroughMakeChunk — переполненная очередь доезжает до
// вызывающего MakeChunk ОТКАЗОМ, а не пустотой и не ожиданием, и не оставляет
// полёта, к которому прицепился бы следующий запрос.
func TestQueueOverflowRefusesThroughMakeChunk(t *testing.T) {
	_, lazy := newLazy(t)
	for i := 0; i < computeSlots; i++ {
		lazy.limiter.compute <- struct{}{}
	}
	for i := 0; i < queueSlots; i++ {
		lazy.limiter.queue <- struct{}{}
	}

	a := chunk.Address{Region: "ST_A", Level: 0, CX: 2, CZ: -1}
	if !lazy.sel.inExtent(a) {
		t.Fatalf("%v: клетка вне домена — проверять нечего", a)
	}
	done := make(chan error, 1)
	go func() {
		_, ok, err := lazy.MakeChunk(a, 1)
		if ok {
			done <- errors.New("переполненная очередь посчитала чанк")
			return
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrQueueFull) {
			t.Fatalf("отказ %v, ожидался ErrQueueFull", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("переполненная очередь не отказала — запрос повис")
	}

	lazy.flights.mu.Lock()
	left := len(lazy.flights.in)
	lazy.flights.mu.Unlock()
	if left != 0 {
		t.Fatalf("после отказа в учёте осталось %d полётов", left)
	}
}
