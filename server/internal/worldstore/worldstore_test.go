package worldstore

import (
	"database/sql"
	"fmt"
	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/track"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("открытие: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// testRule — правило подробности тестового региона: то же, что у фикстуры
// seedmap. Регион без правила хранилище не принимает — про его чанки нельзя
// было бы сказать, каким охватом они порождены.
var testRule = chunk.Rule{Level0RadiusM: 512, MaxLevel: 4}

// testDomain — домен тестового региона: тот же, что у фикстуры seedmap.
// Регион без домена хранилище не принимает — про границы его мира нельзя
// было бы сказать ничего.
var testDomain = mapfmt.Domain{MinX: -8192, MinZ: -12288, MaxX: 12288, MaxZ: 12288}

func putRegion(t *testing.T, s *Store) {
	t.Helper()
	if err := s.PutRegion(Region{ID: "ST_A", Frame: `{"datum":"WGS84"}`, Epoch: 1, Rule: testRule, Domain: testDomain}); err != nil {
		t.Fatalf("регион: %v", err)
	}
}

// Правило подробности переживает запись и чтение. Проверяется отдельно от
// прочих полей затем, что по нему клиент выбирает, какой уровень спрашивать:
// потерянное при чтении, оно стало бы «правилом неизвестно» и остановило бы
// сервер на старте.
func TestRegionRuleSurvivesRoundTrip(t *testing.T) {
	s := openStore(t)
	putRegion(t, s)
	got, ok, err := s.GetRegion("ST_A")
	if err != nil || !ok {
		t.Fatalf("чтение региона: ok=%v, err=%v", ok, err)
	}
	if got.Rule != testRule {
		t.Fatalf("правило %+v, записано %+v", got.Rule, testRule)
	}
	if got.Domain != testDomain {
		t.Fatalf("домен %+v, записан %+v", got.Domain, testDomain)
	}
	// Регион без правила — отказ, а не запись с нулями: молча записанный ноль
	// стал бы миром радиусом ноль, неотличимым от исправного.
	if err := s.PutRegion(Region{ID: "ST_B", Frame: "{}", Epoch: 1}); err == nil {
		t.Fatal("регион без правила подробности записан — отказа не было")
	}
	// Регион без домена — тот же класс отказа: без домена нельзя сказать, где
	// мир кончается, и манифест не смог бы ответить клиенту.
	if err := s.PutRegion(Region{ID: "ST_B", Frame: "{}", Epoch: 1, Rule: testRule}); err == nil {
		t.Fatal("регион без домена записан — отказа не было")
	}
}

func newField(t *testing.T) *terrain.Field {
	t.Helper()
	m := seedmap.Station(seedmap.WithTerrain())
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("фикстура невалидна: %v", err)
	}
	_, els, err := track.Propagate(m)
	if err != nil {
		t.Fatalf("распространение: %v", err)
	}
	field, err := terrain.New(m, els)
	if err != nil {
		t.Fatalf("рельеф: %v", err)
	}
	return field
}

// testWorldVersion — версия мира тестовых фикстур: строки живут под ней, а
// версионные тесты кладут рядом вторую.
const testWorldVersion int64 = 1

func putChunk(t *testing.T, s *Store, f *terrain.Field, a chunk.Address) {
	t.Helper()
	h, err := f.ChunkHeights(a)
	if err != nil {
		t.Fatalf("отсчёты чанка: %v", err)
	}
	err = s.PutChunk(Chunk{
		Address:      a,
		WorldVersion: testWorldVersion,
		Revision:     1,
		BaseZmm:      int64(f.BaseZ() * 1000),
		Heights:      h,
	})
	if err != nil {
		t.Fatalf("запись чанка: %v", err)
	}
}

// СКВОЗНАЯ ПРОВЕРКА КОНТРАКТА. Два соседних чанка порождаются и пишутся
// ПОРОЗНЬ, а на стыке обязаны сойтись бит в бит: последний столбец левого — это
// нулевой столбец правого. Без этого между чанками остаётся щель в один
// интервал, и в неё видно небо.
//
// Проверка идёт через базу намеренно: она ловит не только арифметику сетки, но
// и кодирование блоба — порядок байт и порядок обхода.
func TestSharedRowAgreesThroughStore(t *testing.T) {
	s := openStore(t)
	putRegion(t, s)
	f := newField(t)

	left := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 0}
	right := chunk.Address{Region: "ST_A", Level: 0, CX: 1, CZ: 0}
	below := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 1}
	for _, a := range []chunk.Address{left, right, below} {
		putChunk(t, s, f, a)
	}

	leftChunk, ok, err := s.GetChunk(left, testWorldVersion)
	if err != nil || !ok {
		t.Fatalf("левый чанк: ok=%v err=%v", ok, err)
	}
	rightChunk, _, _ := s.GetChunk(right, testWorldVersion)
	belowChunk, _, _ := s.GetChunk(below, testWorldVersion)

	for j := range chunk.Samples {
		a := leftChunk.Heights[chunk.Index(chunk.Samples-1, j)]
		b := rightChunk.Heights[chunk.Index(0, j)]
		if a != b {
			t.Fatalf("столбец %d: левый %d см, правый %d см", j, a, b)
		}
	}
	for i := range chunk.Samples {
		a := leftChunk.Heights[chunk.Index(i, chunk.Samples-1)]
		b := belowChunk.Heights[chunk.Index(i, 0)]
		if a != b {
			t.Fatalf("ряд %d: верхний %d см, нижний %d см", i, a, b)
		}
	}
}

// Разреженность — свойство хранилища, а не сбой: спросивший пустоту получает
// «здесь ничего нет», а не отказ.
func TestMissingChunkIsNotAnError(t *testing.T) {
	s := openStore(t)
	putRegion(t, s)

	c, ok, err := s.GetChunk(chunk.Address{Region: "ST_A", Level: 0, CX: 9999, CZ: -9999}, testWorldVersion)
	if err != nil {
		t.Fatalf("отсутствующий чанк вернул ошибку: %v", err)
	}
	if ok {
		t.Fatalf("отсутствующий чанк нашёлся: %+v", c)
	}
}

// Блоб переживает запись и чтение без потерь, включая отрицательные отсчёты:
// int16 знаковый, и ошибка в знаке дала бы выемки высотой в 655 метров.
func TestBlobSurvivesRoundTrip(t *testing.T) {
	s := openStore(t)
	putRegion(t, s)

	h := make([]int16, chunk.Samples*chunk.Samples)
	for i := range h {
		h[i] = int16(i%1000) - 500
	}
	a := chunk.Address{Region: "ST_A", Level: 3, CX: -7, CZ: 11}
	if err := s.PutChunk(Chunk{Address: a, WorldVersion: testWorldVersion, Revision: 2, BaseZmm: 140000, Heights: h}); err != nil {
		t.Fatalf("запись: %v", err)
	}
	got, ok, err := s.GetChunk(a, testWorldVersion)
	if err != nil || !ok {
		t.Fatalf("чтение: ok=%v err=%v", ok, err)
	}
	for i := range h {
		if got.Heights[i] != h[i] {
			t.Fatalf("отсчёт %d: %d вместо %d", i, got.Heights[i], h[i])
		}
	}
	if got.Revision != 2 || got.BaseZmm != 140000 {
		t.Fatalf("ревизия %d, база %d мм", got.Revision, got.BaseZmm)
	}
	if len(got.Hash) != 64 {
		t.Fatalf("хеш длиной %d, ожидался sha256 в hex", len(got.Hash))
	}
}

// Хеш служит ETag'ом, значит обязан меняться вместе с телом и не меняться без
// него.
func TestHashFollowsBody(t *testing.T) {
	s := openStore(t)
	putRegion(t, s)
	a := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 0}

	h := make([]int16, chunk.Samples*chunk.Samples)
	put := func(base int64) string {
		t.Helper()
		if err := s.PutChunk(Chunk{Address: a, WorldVersion: testWorldVersion, Revision: 1, BaseZmm: base, Heights: h}); err != nil {
			t.Fatalf("запись: %v", err)
		}
		c, _, _ := s.GetChunk(a, testWorldVersion)
		return c.Hash
	}

	first := put(140000)
	if put(140000) != first {
		t.Fatal("хеш изменился без изменения тела")
	}
	h[42] = 7
	if put(140000) == first {
		t.Fatal("хеш не изменился при изменении отсчёта")
	}
	h[42] = 0
	if put(999000) == first {
		t.Fatal("хеш не изменился при смене опорной высоты")
	}
}

// Ссылка чанка на несуществующий регион не должна проходить: без включённых
// внешних ключей SQLite пропустил бы её молча.
func TestChunkWithoutRegionIsRejected(t *testing.T) {
	s := openStore(t)
	h := make([]int16, chunk.Samples*chunk.Samples)
	err := s.PutChunk(Chunk{
		Address:      chunk.Address{Region: "НЕТ_ТАКОГО", Level: 0},
		WorldVersion: testWorldVersion,
		Heights:      h,
	})
	if err == nil {
		t.Fatal("чанк без региона записан")
	}
}

// TestConcurrentWritersDoNotCollide — база выдерживает столько писателей,
// сколько к серверу пришло запросов.
//
// # Почему это появилось только теперь
//
// До порождения чанков по требованию писал ОДИН поток на старте, и умолчаний
// SQLite хватало с запасом. Как только чанк считается по запросу, писателей
// становится столько же, сколько одновременных запросов, и первый же залп
// клиента упирался в `database is locked (SQLITE_BUSY)`: замер на этой самой
// проверке до настройки соединения давал 63 отказа из 512.
//
// # Что именно проверяется
//
// Не «нет гонок» — это дело -race, — а то, что настройки доехали до КАЖДОГО
// соединения пула. database/sql открывает соединения по мере надобности, и
// PRAGMA, выполненная разово через Exec, достаётся ровно одному из них;
// остальные приходят с умолчаниями. Одна горутина такого не покажет никогда:
// ей хватает первого соединения.
func TestConcurrentWritersDoNotCollide(t *testing.T) {
	s := openStore(t)
	putRegion(t, s)
	h := make([]int16, chunk.Samples*chunk.Samples)

	const writers, each = 32, 8
	errs := make(chan error, writers*each*2)
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range each {
				a := chunk.Address{Region: "ST_A", Level: 0, CX: w, CZ: n}
				if err := s.PutChunk(Chunk{Address: a, WorldVersion: testWorldVersion, Revision: 1, BaseZmm: 1000, Heights: h}); err != nil {
					errs <- err
					return
				}
				if _, ok, err := s.GetChunk(a, testWorldVersion); err != nil {
					errs <- err
					return
				} else if !ok {
					errs <- fmt.Errorf("чанк %v записан и не прочитан", a)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)

	failed := 0
	for err := range errs {
		if failed == 0 {
			t.Errorf("первый отказ: %v", err)
		}
		failed++
	}
	if failed != 0 {
		t.Fatalf("отказов %d из %d записей", failed, writers*each)
	}

	// Внешние ключи обязаны действовать на ВСЕХ соединениях, а не только на
	// первом: пул к этой минуте уже разросся, и запись мимо региона, прошедшая
	// на одном из соединений, завела бы чанк-сироту.
	var wg2 sync.WaitGroup
	slipped := make(chan struct{}, writers)
	for range writers {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			err := s.PutChunk(Chunk{Address: chunk.Address{Region: "НЕТ_ТАКОГО", Level: 0}, WorldVersion: testWorldVersion, Heights: h})
			if err == nil {
				slipped <- struct{}{}
			}
		}()
	}
	wg2.Wait()
	close(slipped)
	if n := len(slipped); n != 0 {
		t.Fatalf("чанк без региона прошёл %d раз: внешние ключи достались не каждому соединению", n)
	}
}

// Пачка чанков пишется ОДНОЙ транзакцией: группа согласованности обязана
// выйти целиком или не выйти вовсе — частичная группа выглядела бы
// опубликованной, и читатель поймал бы половину шва.
func TestPutChunksBatchWritesAndRollsBack(t *testing.T) {
	s := openStore(t)
	putRegion(t, s)
	f := newField(t)

	addr := func(cx, cz int) chunk.Address {
		return chunk.Address{Region: "ST_A", Level: 0, CX: cx, CZ: cz}
	}
	good := func(cx, cz int) Chunk {
		h, err := f.ChunkHeights(addr(cx, cz))
		if err != nil {
			t.Fatalf("отсчёты: %v", err)
		}
		return Chunk{Address: addr(cx, cz), WorldVersion: testWorldVersion, Revision: 1, BaseZmm: 140000, Heights: h}
	}

	// Пачка с негодным членом: валидные члены не должны выжить — откат обязан
	// быть полным, иначе промахнувшаяся группа запишет половину себя.
	bad := good(2, 2)
	bad.Cover = []byte{1, 2, 3} // не CoverBytes — отказ в записи
	if err := s.PutChunks([]Chunk{good(0, 0), bad, good(1, 0)}); err == nil {
		t.Fatal("пачка с негодным покровом записана без отказа")
	}
	for _, a := range []chunk.Address{addr(0, 0), addr(1, 0), addr(2, 2)} {
		if _, ok, err := s.GetChunk(a, testWorldVersion); err != nil {
			t.Fatalf("чтение %v: %v", a, err)
		} else if ok {
			t.Fatalf("чанк %v выжил после отката пачки", a)
		}
	}

	// Годная пачка пишется вся, и каждый член читается.
	if err := s.PutChunks([]Chunk{good(0, 0), good(1, 0)}); err != nil {
		t.Fatalf("пачка: %v", err)
	}
	for _, a := range []chunk.Address{addr(0, 0), addr(1, 0)} {
		if _, ok, err := s.GetChunk(a, testWorldVersion); err != nil || !ok {
			t.Fatalf("чанк %v после пачки: ok=%v err=%v", a, ok, err)
		}
	}

	// Пустая пачка — согласованный no-op: группа без адресов к записи.
	if err := s.PutChunks(nil); err != nil {
		t.Fatalf("пустая пачка: %v", err)
	}
}

// TestJournalAdvancesWithoutWorldVersion — ГЛАВНЫЙ ТЕСТ ЗАДАЧИ: версия мира
// отделена от журнала нарочно (спека §6.3). Движение машиниста — команда,
// которая журнал двигает, а землю не меняет: продвинулся source_journal_seq,
// версия мира стояла. Смешай их — и каждая команда дала бы новый корень мира,
// и клиент перезагружал бы неизменившийся мир.
func TestJournalAdvancesWithoutWorldVersion(t *testing.T) {
	s := openStore(t)
	region := Region{ID: "ST_A", Frame: "{}", Epoch: 1, Rule: testRule, Domain: testDomain}
	if err := s.Seed(region, ProjectionHead{WorldVersion: 1, SourceJournalSeq: 5, NetworkVersion: 1, RegionRecipeHash: "r1"}, nil); err != nil {
		t.Fatalf("засев: %v", err)
	}

	head, ok, err := s.GetProjectionHead("ST_A")
	if err != nil || !ok {
		t.Fatalf("голова: ok=%v err=%v", ok, err)
	}
	// Машинист повернул рукоятку: команда №6 в журнале, земля не тронута.
	head.SourceJournalSeq = 6
	if err := s.PutProjectionHead("ST_A", head); err != nil {
		t.Fatalf("продвижение журнала: %v", err)
	}
	got, _, err := s.GetProjectionHead("ST_A")
	if err != nil {
		t.Fatalf("чтение головы: %v", err)
	}
	if got.WorldVersion != 1 {
		t.Fatalf("версия мира %d, ожидалась 1: команда без земляного эффекта породила новый корень мира", got.WorldVersion)
	}
	if got.SourceJournalSeq != 6 {
		t.Fatalf("журнал %d, ожидался 6: команда не долетела до головы", got.SourceJournalSeq)
	}
}

// TestStaleWriterCannotOverwriteNewVersion — устаревший писатель обезвреживается
// КЛЮЧОМ СТРОКИ, а не транзакцией сравнения (спека §6.3). Запрос, начавший счёт
// при версии 1 и закончивший после публикации версии 2, дописывает СВОЮ версию:
// новая земля в ключе (addr, 2) лежит, а не затёрта, и чтение «по состоянию
// на версию 2» возвращает ровно её байты.
func TestStaleWriterCannotOverwriteNewVersion(t *testing.T) {
	s := openStore(t)
	putRegion(t, s)
	a := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 0}
	h := make([]int16, chunk.Samples*chunk.Samples)
	for i := range h {
		h[i] = 1
	}
	fresh := make([]int16, len(h))
	for i := range fresh {
		fresh[i] = 2
	}

	// Публикация 2 (новая земля) — первой, как и случается: правка пришла,
	// пока устаревший запрос считал.
	if err := s.PutChunk(Chunk{Address: a, WorldVersion: 2, Revision: 1, BaseZmm: 140000, Heights: fresh}); err != nil {
		t.Fatalf("новая земля: %v", err)
	}
	// Устаревший писатель дописывает СВОЮ версию 1.
	if err := s.PutChunk(Chunk{Address: a, WorldVersion: 1, Revision: 1, BaseZmm: 140000, Heights: h}); err != nil {
		t.Fatalf("устаревшая запись: %v", err)
	}

	got, ok, err := s.GetChunk(a, 2)
	if err != nil || !ok {
		t.Fatalf("чтение версии 2: ok=%v err=%v", ok, err)
	}
	if got.WorldVersion != 2 {
		t.Fatalf("чтение версии 2 вернуло строку версии %d — новая земля затёрта", got.WorldVersion)
	}
	if got.Heights[0] != 2 {
		t.Fatalf("новая земля версии 2 испорчена: отсчёт %d вместо 2", got.Heights[0])
	}
	// Обе строки лежат рядом: версия 1 по-прежнему читается как своя.
	old, ok, err := s.GetChunk(a, 1)
	if err != nil || !ok {
		t.Fatalf("чтение версии 1: ok=%v err=%v", ok, err)
	}
	if old.WorldVersion != 1 || old.Heights[0] != 1 {
		t.Fatalf("версия 1 испорчена: версия %d, отсчёт %d", old.WorldVersion, old.Heights[0])
	}
	// Чтение «по состоянию на 3» отдаёт последнюю записанную не старше — 2.
	later, ok, err := s.GetChunk(a, 3)
	if err != nil || !ok {
		t.Fatalf("чтение на версию 3: ok=%v err=%v", ok, err)
	}
	if later.WorldVersion != 2 {
		t.Fatalf("чтение на версию 3 вернуло версию %d, ожидалась 2", later.WorldVersion)
	}
}

// TestMigrationAddsWorldVersionToChunkKey — база ПРЕЖНЕЙ СБОРКИ (ключ чанка без
// версии) открывается новой и перестраивает таблицу: строки получают версию 1 —
// мир, засеянный прежней сборкой, это и есть первая публикация. Форма старой
// базы воспроизведена дословно по тому, что писала прошлая сборка: те же
// таблицы, те же колонки, ключ (region, level, cx, cz) без версии.
func TestMigrationAddsWorldVersionToChunkKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	blob, err := chunk.EncodeHeights(make([]int16, chunk.Samples*chunk.Samples))
	if err != nil {
		t.Fatalf("отсчёты: %v", err)
	}
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("старая база: %v", err)
	}
	_, err = old.Exec(`
		CREATE TABLE regions (
			id              TEXT PRIMARY KEY,
			frame           TEXT NOT NULL,
			provenance      TEXT NOT NULL DEFAULT '',
			redistributable INTEGER NOT NULL DEFAULT 0,
			epoch           INTEGER NOT NULL DEFAULT 1,
			level0_radius_m REAL NOT NULL DEFAULT 0,
			max_level       INTEGER NOT NULL DEFAULT -1,
			domain_min_x    REAL NOT NULL DEFAULT 0,
			domain_min_z    REAL NOT NULL DEFAULT 0,
			domain_max_x    REAL NOT NULL DEFAULT 0,
			domain_max_z    REAL NOT NULL DEFAULT 0
		);
		CREATE TABLE chunks (
			region    TEXT    NOT NULL REFERENCES regions(id),
			level     INTEGER NOT NULL,
			cx        INTEGER NOT NULL,
			cz        INTEGER NOT NULL,
			revision  INTEGER NOT NULL,
			hash      TEXT    NOT NULL,
			base_z_mm INTEGER NOT NULL,
			heights   BLOB    NOT NULL,
			cover     BLOB,
			forest    BLOB,
			PRIMARY KEY (region, level, cx, cz)
		);
		INSERT INTO regions (id, frame, epoch, level0_radius_m, max_level,
			domain_min_x, domain_min_z, domain_max_x, domain_max_z)
		VALUES ('OLD', '{}', 1, 512, 4, -8192, -12288, 12288, 12288);
		INSERT INTO chunks (region, level, cx, cz, revision, hash, base_z_mm, heights)
		VALUES ('OLD', 0, 0, 0, 1, 'deadbeef', 140000, ?);
	`, blob)
	if err != nil {
		old.Close()
		t.Fatalf("старая схема: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("закрытие: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("открытие новой сборкой: %v", err)
	}
	c, ok, err := s.GetChunk(chunk.Address{Region: "OLD", Level: 0, CX: 0, CZ: 0}, 1)
	if err != nil || !ok {
		s.Close()
		t.Fatalf("чанк после миграции: ok=%v err=%v", ok, err)
	}
	if c.WorldVersion != 1 {
		s.Close()
		t.Fatalf("версия %d, ожидалась 1 — миграция не назвала строке первую публикацию", c.WorldVersion)
	}
	// Повторное открытие не перестраивает таблицу повторно: колонка уже есть,
	// и строка на месте.
	if err := s.Close(); err != nil {
		t.Fatalf("закрытие: %v", err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("повторное открытие: %v", err)
	}
	defer s2.Close()
	if _, ok, err := s2.GetChunk(chunk.Address{Region: "OLD", Level: 0, CX: 0, CZ: 0}, 1); err != nil || !ok {
		t.Fatalf("чанк после повторного открытия: ok=%v err=%v", ok, err)
	}
}

// TestPublishEmptyIsRefused — пустая публикация (ни строк, ни сети) — отказ, а
// не тихий шаг версии: версия мира обязана что-то публиковать, иначе счётчик
// растёт без причины и клиент перезагружает неизменившийся мир.
func TestPublishEmptyIsRefused(t *testing.T) {
	s := openStore(t)
	region := Region{ID: "ST_A", Frame: "{}", Epoch: 1, Rule: testRule, Domain: testDomain}
	if err := s.Seed(region, ProjectionHead{WorldVersion: 1, SourceJournalSeq: 0, NetworkVersion: 1, RegionRecipeHash: "r1"}, nil); err != nil {
		t.Fatalf("засев: %v", err)
	}
	if _, err := s.Publish("ST_A", nil, nil, 0); err == nil {
		t.Fatal("пустая публикация не отвергнута")
	}
	head, _, err := s.GetProjectionHead("ST_A")
	if err != nil {
		t.Fatalf("голова: %v", err)
	}
	if head.WorldVersion != 1 {
		t.Fatalf("версия %d, ожидалась 1: пустая публикация сдвинула голову", head.WorldVersion)
	}
}

// TestPutProjectionHeadRefusesJournalRegression — откат source_journal_seq при
// равной версии — отказ (бида sqym.20): смысл поля «до какой команды
// построено», и младший номер при той же версии сделал бы ответ головы ложным.
func TestPutProjectionHeadRefusesJournalRegression(t *testing.T) {
	s := openStore(t)
	region := Region{ID: "ST_A", Frame: "{}", Epoch: 1, Rule: testRule, Domain: testDomain}
	if err := s.Seed(region, ProjectionHead{WorldVersion: 2, SourceJournalSeq: 7, NetworkVersion: 1, RegionRecipeHash: "r1"}, nil); err != nil {
		t.Fatalf("засев: %v", err)
	}
	// Равная версия, младший журнал — отказ, и голова не меняется.
	back := ProjectionHead{WorldVersion: 2, SourceJournalSeq: 6, NetworkVersion: 1, RegionRecipeHash: "r1"}
	if err := s.PutProjectionHead("ST_A", back); err == nil {
		t.Fatal("откат журнала при равной версии не отвергнут")
	}
	head, _, err := s.GetProjectionHead("ST_A")
	if err != nil {
		t.Fatalf("голова: %v", err)
	}
	if head.SourceJournalSeq != 7 {
		t.Fatalf("журнал %d, ожидался 7: отвергнутая запись изменила голову", head.SourceJournalSeq)
	}
	// Продвижение журнала при равной версии остаётся законным.
	fwd := ProjectionHead{WorldVersion: 2, SourceJournalSeq: 8, NetworkVersion: 1, RegionRecipeHash: "r1"}
	if err := s.PutProjectionHead("ST_A", fwd); err != nil {
		t.Fatalf("продвижение журнала: %v", err)
	}
}

// TestConcurrentPublishersGetDistinctVersions — ГЛАВНЫЙ ТЕСТ ЗАДАЧИ (бида
// sqym.17): два публикатора, которым номер версии НЕ выдан заранее, публикуют
// ОДНОВРЕМЕННО настоящими горутинами. Прежний код читал голову до db.Begin и
// прибавлял единицу снаружи транзакции: оба публикатора брали ОДИН номер, и
// второй безусловным DO UPDATE затирал строки первого — содержимое под
// immutable-адресом сменилось бы.
//
// Оба публикатора пишут ОДНИ И ТЕ ЖЕ адреса разным содержимым («красным» и
// «синим»), так что потерянная строка различима: на каждом адресе обязаны
// лежать ДВЕ строки — версии 2 и 3 — с разными содержимыми. Версия
// проверяется по WorldVersion прочитанной строки, а не только по содержимому:
// GetChunk при отсутствии строки 3 вернул бы строку 2, и тест ложно прошёл бы.
func TestConcurrentPublishersGetDistinctVersions(t *testing.T) {
	s := openStore(t)
	region := Region{ID: "ST_A", Frame: "{}", Epoch: 1, Rule: testRule, Domain: testDomain}
	if err := s.Seed(region, ProjectionHead{WorldVersion: 1, SourceJournalSeq: 0, NetworkVersion: 1, RegionRecipeHash: "r1"}, nil); err != nil {
		t.Fatalf("засев: %v", err)
	}

	// Несколько адресов растягивают окно гонки прежнего кода: между чтением
	// головы и коммитом укладывается запись всех строк, и второй публикатор
	// успевает прочитать ту же голову.
	const cells = 6
	addrs := make([]chunk.Address, cells)
	for k := range addrs {
		addrs[k] = chunk.Address{Region: "ST_A", Level: 0, CX: k, CZ: 0}
	}
	red := make([]int16, chunk.Samples*chunk.Samples)
	blue := make([]int16, chunk.Samples*chunk.Samples)
	for i := range red {
		red[i] = 1
		blue[i] = 2
	}
	rows := func(h []int16) []Chunk {
		out := make([]Chunk, cells)
		for k, a := range addrs {
			out[k] = Chunk{Address: a, Revision: 1, BaseZmm: 140000, Heights: h}
		}
		return out
	}

	// Стартовый барьер: оба публикатора рвутся к голове одновременно, иначе
	// исход теста зависел бы от того, успел ли первый закончить до старта
	// второго.
	start := make(chan struct{})
	type pub struct {
		head ProjectionHead
		err  error
	}
	results := make(chan pub, 2)
	publish := func(h []int16) {
		<-start
		head, err := s.Publish("ST_A", rows(h), nil, 10)
		results <- pub{head, err}
	}
	go publish(red)
	go publish(blue)
	close(start)

	var heads []ProjectionHead
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatalf("публикатор: %v", r.err)
		}
		heads = append(heads, r.head)
	}
	if heads[0].WorldVersion == heads[1].WorldVersion {
		t.Fatalf("публикаторы получили один номер %d: выдача не атомарна", heads[0].WorldVersion)
	}
	if heads[0].WorldVersion != 2 && heads[1].WorldVersion != 2 {
		t.Fatalf("версии %d и %d, ожидались 2 и 3", heads[0].WorldVersion, heads[1].WorldVersion)
	}
	if heads[0].WorldVersion != 3 && heads[1].WorldVersion != 3 {
		t.Fatalf("версии %d и %d, ожидались 2 и 3", heads[0].WorldVersion, heads[1].WorldVersion)
	}

	head, ok, err := s.GetProjectionHead("ST_A")
	if err != nil || !ok {
		t.Fatalf("голова: ok=%v err=%v", ok, err)
	}
	if head.WorldVersion != 3 {
		t.Fatalf("версия головы %d, ожидалась 3: одна публикация потеряна", head.WorldVersion)
	}
	for _, a := range addrs {
		v2, ok, err := s.GetChunk(a, 2)
		if err != nil || !ok {
			t.Fatalf("адрес %v версия 2: ok=%v err=%v", a, ok, err)
		}
		if v2.WorldVersion != 2 {
			t.Fatalf("адрес %v: строки версии 2 нет (чтение вернуло версию %d)", a, v2.WorldVersion)
		}
		v3, ok, err := s.GetChunk(a, 3)
		if err != nil || !ok {
			t.Fatalf("адрес %v версия 3: ok=%v err=%v", a, ok, err)
		}
		if v3.WorldVersion != 3 {
			t.Fatalf("адрес %v: строки версии 3 нет (чтение вернуло версию %d)", a, v3.WorldVersion)
		}
		if slices.Equal(v2.Heights, v3.Heights) {
			t.Fatalf("адрес %v: версии 2 и 3 несут одно содержимое — одна публикация затёрта", a)
		}
	}
}
