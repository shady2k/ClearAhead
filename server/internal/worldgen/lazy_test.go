package worldgen

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// newLazy поднимает свежую базу с заведённым регионом и порождение по
// требованию над ней. База отдаётся наружу затем, что половина проверок — про
// то, что посчитанное ЛЕГЛО в базу, а не только доехало до вызывающего.
func newLazy(t *testing.T) (*worldstore.Store, *Lazy) {
	t.Helper()
	s := newStore(t)
	l, err := NewLazy(s, newMap(t), "ST_A", 1)
	if err != nil {
		t.Fatalf("порождение по требованию: %v", err)
	}
	return s, l
}

// requireSameChunk сверяет два чанка БАЙТ В БАЙТ, а не по хешу одному.
//
// Хеш проверяется тоже, но он производный: сойдись байты и разойдись хеш — это
// поломка ETag'а, а разойдись байты при совпавшем хеше — поломка самого хеша.
// Названы обе, потому что лечатся они в разных местах.
func requireSameChunk(t *testing.T, name string, got, want worldstore.Chunk) {
	t.Helper()
	if got.BaseZmm != want.BaseZmm {
		t.Fatalf("%s: опорная высота %d мм, ожидалось %d", name, got.BaseZmm, want.BaseZmm)
	}
	gotBlob, err := chunk.EncodeHeights(got.Heights)
	if err != nil {
		t.Fatalf("%s: кодирование: %v", name, err)
	}
	wantBlob, err := chunk.EncodeHeights(want.Heights)
	if err != nil {
		t.Fatalf("%s: кодирование: %v", name, err)
	}
	if !bytes.Equal(gotBlob, wantBlob) {
		diff := 0
		for k := range gotBlob {
			if gotBlob[k] != wantBlob[k] {
				diff++
			}
		}
		t.Fatalf("%s: высоты разошлись в %d байтах из %d", name, diff, len(wantBlob))
	}
	if !bytes.Equal(got.Cover, want.Cover) {
		t.Fatalf("%s: покров разошёлся (%d и %d байт)", name, len(got.Cover), len(want.Cover))
	}
	if !bytes.Equal(got.Forest, want.Forest) {
		t.Fatalf("%s: лес разошёлся (%d и %d байт)", name, len(got.Forest), len(want.Forest))
	}
	if got.Hash != want.Hash {
		t.Fatalf("%s: байты совпали, а хеш разошёлся: %s против %s — сломан ETag", name, got.Hash, want.Hash)
	}
}

// TestOnDemandChunkRepeatsWarmedOneByteForByte — ГЛАВНОЕ ДОКАЗАТЕЛЬСТВО того,
// что база вправе быть кэшем.
//
// Рельеф выводится из рецепта детерминированно, значит чанк, посчитанный по
// запросу через час после старта, обязан выйти тем же, что положил прогрев.
// Обязан — не значит выходит: путь входа у двух дорог общий (prepare), но общий
// он по построению, а не по проверке, и первая же правка, добавившая шаг мимо
// prepare, развела бы их молча. Разошедшийся чанк не роняет ничего: клиент
// увидит шов в рельефе там, где кэш холодный, и спишет его на что угодно.
//
// Сверяются ДВЕ РАЗНЫЕ базы и два разных построения поля — иначе проверка
// сводилась бы к «одна и та же память равна себе».
func TestOnDemandChunkRepeatsWarmedOneByteForByte(t *testing.T) {
	warm := newStore(t)
	rep, err := Generate(warm, newMap(t), "ST_A", 1)
	if err != nil {
		t.Fatalf("прогрев: %v", err)
	}
	t.Logf("прогрев положил %d чанков", rep.TotalChunks)

	_, lazy := newLazy(t)

	// Берутся адреса всех уровней, которые прогрев действительно породил:
	// уровень входит и в развёртку, и в выбор шага координат, и ошибка,
	// живущая только на третьем уровне, на нулевом не видна.
	checked := 0
	for level := 0; level <= lazy.sel.rule.MaxLevel; level++ {
		a := chunk.Address{Region: "ST_A", Level: level, CX: 0, CZ: 0}
		want, ok, err := warm.GetChunk(a)
		if err != nil {
			t.Fatalf("чтение прогретого %v: %v", a, err)
		}
		if !ok {
			continue
		}
		got, ok, err := lazy.MakeChunk(a)
		if err != nil {
			t.Fatalf("счёт по требованию %v: %v", a, err)
		}
		if !ok {
			t.Fatalf("%v: прогрев чанк породил, а по требованию он объявлен пустотой", a)
		}
		requireSameChunk(t, fmt.Sprintf("%s уровень %d", a.Region, level), got, want)
		checked++
	}
	if checked == 0 {
		t.Fatal("не сверено ни одного уровня — проверка прошла бы и при сломанном прогреве")
	}
	t.Logf("сверено уровней: %d", checked)
}

// TestOnDemandFillsDetailWhereWarmupHasNone — то, ради чего всё затевалось.
//
// Три километра от рельсов: прогрев кладёт здесь только грубые уровни, потому
// что подробность он выбирает БЛИЗОСТЬЮ К ПУТИ. Камера же выбирает её тем, куда
// смотрит, и, спустившись к земле, просит уровень 0 — которого нет и не будет.
// Проверка утверждает ровно это: covers про такую клетку говорит «нет», а
// порождение по требованию отдаёт её и кладёт в базу.
func TestOnDemandFillsDetailWhereWarmupHasNone(t *testing.T) {
	store, lazy := newLazy(t)

	// 3 км от оси по z — заведомо вне R_0 (512 м на затравке) и заведомо внутри
	// охвата (8192 м). Обе половины утверждения проверяются, а не берутся на
	// веру: числа приезжают картой и могут смениться вместе с ней.
	a := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 3000 / int(chunk.SideM(0))}
	if lazy.sel.covers(a) {
		t.Fatalf("%v: прогрев эту клетку кладёт сам — проверять нечего", a)
	}
	if !lazy.sel.inExtent(a) {
		t.Fatalf("%v: клетка за охватом мира — проверять нечего", a)
	}
	if _, ok, err := store.GetChunk(a); err != nil {
		t.Fatalf("чтение: %v", err)
	} else if ok {
		t.Fatalf("%v: чанк уже в базе до счёта", a)
	}

	got, ok, err := lazy.MakeChunk(a)
	if err != nil {
		t.Fatalf("счёт: %v", err)
	}
	if !ok {
		t.Fatalf("%v: подробность внутри охвата объявлена пустотой", a)
	}

	// Посчитанное ОБЯЗАНО лечь в базу: без этого дороже становится не первый
	// запрос, а каждый.
	cached, ok, err := store.GetChunk(a)
	if err != nil {
		t.Fatalf("чтение из базы: %v", err)
	}
	if !ok {
		t.Fatalf("%v: посчитано, но в базу не легло — база не стала кэшем", a)
	}
	requireSameChunk(t, "кэш", cached, got)

	// И то же самое, посчитанное ДРУГИМ полем над ДРУГОЙ базой, обязано выйти
	// байт в байт тем же: детерминизм рецепта не зависит от того, кто его
	// развернул.
	_, other := newLazy(t)
	twin, ok, err := other.MakeChunk(a)
	if err != nil || !ok {
		t.Fatalf("второй счёт: ok=%v, %v", ok, err)
	}
	requireSameChunk(t, "второе построение поля", twin, got)
}

// TestOnDemandStopsAtWorldExtent — охват остаётся охватом.
//
// Порождение по требованию не отменяет ключа terrain.extent: он объявляет,
// докуда мир существует ВООБЩЕ. За пределом — пустота, а не бесконечная земля,
// и это разные ответы: пустота законна, а земля, которой карта не обещала, —
// враньё, которое клиент не сможет отличить от настоящей.
func TestOnDemandStopsAtWorldExtent(t *testing.T) {
	store, lazy := newLazy(t)
	reach := lazy.sel.rule.ReachM()

	// Вдвое дальше охвата — заведомо снаружи при любом округлении.
	a := chunk.Address{Region: "ST_A", Level: 0, CZ: int(2*reach/chunk.SideM(0)) + 1}
	c, ok, err := lazy.MakeChunk(a)
	if err != nil {
		t.Fatalf("%v: за краем мира получен отказ, а ожидалась пустота: %v", a, err)
	}
	if ok {
		t.Fatalf("%v: за краем мира (%.0f м) посчитана земля", a, reach)
	}
	if c.Hash != "" {
		t.Fatalf("%v: пустота приехала с содержимым", a)
	}
	// И в базу ничего не легло: иначе край мира зарастал бы кэшем.
	if _, ok, err := store.GetChunk(a); err != nil {
		t.Fatalf("чтение: %v", err)
	} else if ok {
		t.Fatalf("%v: за краем мира чанк записан в базу", a)
	}
}

// TestOnDemandRefusesAddressOutsideItsWorld — отказ отличается от пустоты.
//
// Уровень, которого у региона нет, и регион, рецепта которого у сервера нет, —
// это не «земли здесь не бывает», а «спрошено не то». Ответь на них пустотой —
// и опечатка в адресе стала бы неотличима от края мира, а сервер без карты
// выдавал бы ровную землю за настоящую.
func TestOnDemandRefusesAddressOutsideItsWorld(t *testing.T) {
	_, lazy := newLazy(t)
	for name, a := range map[string]chunk.Address{
		"уровень выше объявленного": {Region: "ST_A", Level: lazy.sel.rule.MaxLevel + 1},
		"отрицательный уровень":     {Region: "ST_A", Level: -1},
		"чужой регион":              {Region: "НЕТ_ТАКОГО", Level: 0},
	} {
		if _, ok, err := lazy.MakeChunk(a); err == nil {
			t.Fatalf("%s (%v): ok=%v, отказа нет", name, a, ok)
		}
	}
}

// TestConcurrentRequestsComputeChunkOnce — одновременные запросы на один адрес.
//
// Клиент шлёт запросы пачками, и два запроса на один адрес приходят разом
// штатно. Без одиночного полёта каждый считал бы свои 2.7 мс, а писали бы они
// в одну строку базы.
//
// Проверяется СЧЁТ вызовов, а не время: время на загруженной машине сказало бы
// что угодно. Ведущий держится в счёте, пока не отпустят, — этого достаточно,
// чтобы остальные заведомо застали полёт идущим.
func TestConcurrentRequestsComputeChunkOnce(t *testing.T) {
	var g flightGroup
	a := chunk.Address{Region: "ST_A", Level: 0, CX: 7, CZ: -3}

	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	work := func() (worldstore.Chunk, bool, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		<-release
		return worldstore.Chunk{Address: a, Hash: "один"}, true, nil
	}

	const askers = 32
	var wg sync.WaitGroup
	results := make([]worldstore.Chunk, askers)
	for k := range askers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, ok, err := g.do(a, work)
			if err != nil || !ok {
				t.Errorf("спрашивающий %d: ok=%v, %v", k, ok, err)
			}
			results[k] = c
		}()
	}

	// Ведущий уже внутри work и держит адрес; остальные либо ждут его, либо
	// вот-вот встанут в очередь. Пауза даёт им дойти до карты полётов: без неё
	// проверка прошла бы и при отсутствии одиночного полёта — просто потому,
	// что горутины не успели стартовать.
	waitFor(t, &mu, &calls, 1)
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 1 {
		t.Fatalf("чанк посчитан %d раз при %d одновременных запросах, ожидался один счёт", got, askers)
	}
	for k, c := range results {
		if c.Hash != "один" {
			t.Fatalf("спрашивающий %d получил чужой ответ: %q", k, c.Hash)
		}
	}

	// Полёт обязан сняться с учёта: иначе следующий запрос на тот же адрес
	// прицепится к завершённому полёту и получит его ответ навсегда.
	g.mu.Lock()
	left := len(g.in)
	g.mu.Unlock()
	if left != 0 {
		t.Fatalf("после посадки в учёте осталось %d полётов", left)
	}
}

// TestFlightSurvivesPanicOfLeader — сорвавшийся счёт не вешает ждущих.
//
// Повисшая на канале горутина не даёт о себе знать ничем: она держит соединение
// и молчит. Поэтому паника ведущего обязана стать ОТКАЗОМ для ждущих — не
// пустотой, которая выглядела бы законным «земли здесь нет».
func TestFlightSurvivesPanicOfLeader(t *testing.T) {
	var g flightGroup
	a := chunk.Address{Region: "ST_A", Level: 0}
	release := make(chan struct{})

	go func() {
		defer func() { _ = recover() }()
		_, _, _ = g.do(a, func() (worldstore.Chunk, bool, error) {
			<-release
			panic("рецепт сломался")
		})
	}()

	waitForFlight(t, &g, a)
	done := make(chan error, 1)
	go func() {
		_, ok, err := g.do(a, func() (worldstore.Chunk, bool, error) {
			return worldstore.Chunk{}, true, nil
		})
		if ok {
			done <- nil
			return
		}
		done <- err
	}()
	// Ждущий должен стоять на канале полёта, а не считать сам.
	time.Sleep(50 * time.Millisecond)
	close(release)

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("паника ведущего доехала до ждущего как законная пустота или как успех")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ждущий повис: паника ведущего не закрыла полёт")
	}
}

// waitFor крутится, пока счётчик не дойдёт до want. Опрос, а не канал: считает
// его посторонняя функция, у которой своих каналов нет.
func waitFor(t *testing.T, mu *sync.Mutex, counter *int, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := *counter
		mu.Unlock()
		if got >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("не дождались %d вызовов счёта", want)
}

// waitForFlight крутится, пока адрес не окажется в учёте полётов.
func waitForFlight(t *testing.T, g *flightGroup, a chunk.Address) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		g.mu.Lock()
		_, ok := g.in[a]
		g.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("полёт так и не начался")
}

// TestLazyRefusesWhenRegionRuleDiffers — правило подробности региона обязано
// совпасть с правилом карты, иначе сервер не поднимается.
//
// Это та же проверка, что у прогрева (sameRule), и стоит она здесь потому, что
// чанк, посчитанный по требованию, ложится В ТУ ЖЕ базу, что и прогретый.
// Разойдись правила — и в одной базе оказались бы клетки двух разных миров,
// различить которые снаружи нечем: адрес у них одинаковый.
func TestLazyRefusesWhenRegionRuleDiffers(t *testing.T) {
	s, err := worldstore.Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	other := ruleOf(t, newMap(t))
	other.MaxLevel++ // мир на уровень шире, чем обещает карта
	if err := s.PutRegion(worldstore.Region{ID: "ST_A", Frame: "{}", Epoch: 1, Rule: other}); err != nil {
		t.Fatalf("регион: %v", err)
	}
	if _, err := NewLazy(s, newMap(t), "ST_A", 1); err == nil {
		t.Fatal("порождение построено на регионе с чужим правилом подробности")
	}
}
