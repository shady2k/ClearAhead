// Package worldstore — рантайм-хранилище мира: регионы и чанки в SQLite.
//
// # Что меняется по сравнению с прежним устройством
//
// Было: карта — файл JSON, mapstore держит одну карту в памяти целиком. Стало:
// рантайм читает БД почанково и лениво, а JSON остаётся обменным и авторским
// форматом — импорт, экспорт, дифф, фикстуры, ручное авторство небольшой
// станции (`world-storage` §6).
//
// Инвариант mapstore переносится сюда дословно: **в базу попадает только то,
// что прошло полный путь входа** — разбор, валидацию, компиляцию. Файл,
// который нельзя загрузить, не пишется; запись, которую нельзя было получить
// законно, не появляется.
//
// # Почему SQLite, а не голое KV
//
// Для «ключ → блоб» подошёл бы bbolt. Но в базе будет жить не только рельеф:
// сущности со своими идентификаторами, журнал мутаций, происхождение импорта,
// отмена в редакторе, выборка «что попадает в прямоугольник» — это запросы,
// индексы и транзакции. Драйвер `modernc.org/sqlite` — чистый Go: cgo-драйвер
// сломал бы кросс-сборку и обещание «сервер — один бинарник»
// (`world-storage` §5).
//
// # Что здесь есть и чего нет
//
// Есть регионы и статический ярус чанков — у них есть и производитель
// (`internal/terrain`), и потребитель (отдача высот).
//
// НЕТ таблиц журнала мутаций, сущностей, элементов пути и зон, хотя набросок
// схемы в `world-storage` §5 их перечисляет. У них сегодня нет ни одного
// производителя: мутаций не порождает никто, пока нет партии; пространственный
// индекс пути некому спрашивать, пока нет ленивой подгрузки. Заводить таблицы
// под них сейчас значило бы объявить форму без потребителя
// (`map-format-design` §8). Они приезжают вместе со своими производителями.
package worldstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/chunk"

	_ "modernc.org/sqlite"
)

// schema — форма базы.
//
// Ревизия и хеш живут У ЧАНКА, а не у карты: на регионе размером с край любая
// правка обесценивала бы кэш всего мира. Манифест региона несёт эпоху, а не
// поштучный список ревизий (`world-storage` §4).
const schema = `
CREATE TABLE IF NOT EXISTS regions (
    id              TEXT PRIMARY KEY,
    frame           TEXT NOT NULL,
    provenance      TEXT NOT NULL DEFAULT '',
    redistributable INTEGER NOT NULL DEFAULT 0,
    epoch           INTEGER NOT NULL DEFAULT 1,
    level0_radius_m REAL NOT NULL DEFAULT 0,
    max_level       INTEGER NOT NULL DEFAULT -1
) STRICT;

CREATE TABLE IF NOT EXISTS chunks (
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
) STRICT;
`

// migrations — добавления к схеме, применяемые к уже существующей базе.
//
// Отдельно от schema, потому что CREATE TABLE IF NOT EXISTS молча пропускает
// таблицу, у которой не хватает столбца: база, созданная прежней сборкой,
// открылась бы без ошибки и падала бы на первом же запросе к новому столбцу.
//
// Ошибка «duplicate column name» здесь ОЖИДАЕМА и глушится: SQLite не умеет
// ADD COLUMN IF NOT EXISTS, а спрашивать PRAGMA table_info перед каждым
// добавлением — это тот же разбор ошибки, только длиннее и с гонкой.
//
// Покров ДОПУСКАЕТ NULL намеренно: карта без рецепта покрова законна, и
// столбец обязан отличать «покрова нет» от «покров пустой». NOT NULL DEFAULT
// x” сделал бы их неразличимыми.
//
// Умолчания правила подробности выбраны НЕВОЗМОЖНЫМИ (радиус 0, уровень −1)
// нарочно: база, засеянная сборкой без этих столбцов, обязана быть узнана как
// «правило неизвестно» и отвергнута (chunk.Rule.Known, worldgen.sameRule).
// Правдоподобное умолчание — прежние 512 и 4 — выдало бы чанки неизвестного
// происхождения за исправный мир.
var migrations = []string{
	`ALTER TABLE chunks ADD COLUMN cover BLOB`,
	`ALTER TABLE chunks ADD COLUMN forest BLOB`,
	`ALTER TABLE regions ADD COLUMN level0_radius_m REAL NOT NULL DEFAULT 0`,
	`ALTER TABLE regions ADD COLUMN max_level INTEGER NOT NULL DEFAULT -1`,
}

// Store — открытая база мира.
type Store struct{ db *sql.DB }

// dsnPragmas — настройки соединения, без которых база ведёт себя не так, как
// от неё ждут. Едут В СТРОКЕ ПОДКЛЮЧЕНИЯ, а не отдельным `db.Exec("PRAGMA …")`,
// и это не косметика: database/sql держит ПУЛ соединений и открывает новые по
// мере надобности, а PRAGMA действует на одно соединение. Настройка, поданная
// через Exec, достаётся тому соединению, которое пул выдал в тот момент, а
// следующее приходит с умолчаниями — то есть работает ровно до первого
// одновременного запроса и ломается тогда, когда её отсутствие труднее всего
// заметить. Строка подключения применяется к КАЖДОМУ соединению пула.
//
//	foreign_keys   — выключены в SQLite по умолчанию, и ссылка чанка на
//	                 несуществующий регион прошла бы молча. Здесь эта настройка
//	                 и стояла Exec'ом; переезд в DSN — исправление, а не стиль.
//	busy_timeout   — сколько ждать снятия блокировки, прежде чем отказать.
//	journal_mode   — WAL: читатели не ждут писателя, писатель не ждёт читателей.
//	synchronous    — NORMAL: fsync на контрольной точке, а не на каждой записи.
//
// # Замер, ради которого это появилось
//
// 64 горутины по 8 записей и чтений в один файл (M2 Pro, -race):
//
//	было (умолчания, журнал отката): 63 отказа из 512 — `database is locked
//	                                 (SQLITE_BUSY)`;
//	busy_timeout:                    0 отказов, 4.75 с;
//	+ WAL:                           0 отказов, 2.17 с;
//	+ synchronous=NORMAL:            0 отказов, 1.16 с.
//
// До порождения чанка по требованию писал ОДИН поток на старте, и умолчаний
// хватало. Как только чанк считается по запросу, писателей столько же, сколько
// запросов, и первый же залп клиента упирался бы в SQLITE_BUSY.
//
// # Чем оплачено synchronous=NORMAL
//
// При WAL+NORMAL внезапное выключение питания может стоить последних
// транзакций (повреждения базы — нет, это гарантия WAL). Для яруса чанков это
// приемлемо ИМЕННО ПОТОМУ, что база стала кэшем: потерянный чанк выводится из
// рецепта заново и байт в байт. Для таблицы, которую нельзя пересчитать, такой
// размен был бы неправомерен, и вводить его пришлось бы отдельно.
const dsnPragmas = "?_pragma=foreign_keys(1)" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=journal_mode(WAL)" +
	"&_pragma=synchronous(NORMAL)"

// Open открывает или создаёт базу по пути.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+dsnPragmas)
	if err != nil {
		return nil, fmt.Errorf("worldstore: открытие %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("worldstore: схема: %w", err)
	}
	for _, m := range migrations {
		// Ошибка «duplicate column name» ожидаема на уже мигрированной базе и
		// глушится по подстроке. Проверять PRAGMA table_info перед каждым
		// добавлением — тот же разбор ошибки, только длиннее.
		if _, err := db.Exec(m); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			db.Close()
			return nil, fmt.Errorf("worldstore: миграция %q: %w", m, err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Region — запись региона.
//
// Redistributable — не формальность: раздача чанка есть распространение
// данных, из которых он собран. Тот, кто поднял сервер, должен знать, что у
// него внутри (`map-content-design` §11, бида ClearAhead-yu0).
type Region struct {
	ID              string
	Frame           string // блок georeference как JSON
	Provenance      string
	Redistributable bool
	Epoch           int64
	// Rule — правило подробности, КОТОРЫМ ЭТОТ РЕГИОН ЗАСЕЯН.
	//
	// Хранится, а не выводится из карты при каждом чтении, потому что карта на
	// диске сменится, а чанки в базе останутся прежними. Спросить «каким
	// правилом порождено то, что лежит здесь» больше негде: у чанка записаны
	// уровень и координаты, но не радиус, по которому он был отобран.
	//
	// Отсюда же манифест региона берёт числа для клиента — то есть клиент
	// получает правило ХРАНИЛИЩА, а не правило файла карты. Разойтись им не
	// дают на старте (worldgen.sameRule): сервер отказывается подниматься с
	// картой, чей охват не тот, которым засеяна база.
	Rule chunk.Rule
}

// PutRegion записывает или обновляет регион.
//
// Регион без правила подробности не записывается: он был бы регионом, про
// чанки которого нельзя сказать, откуда они взялись.
func (s *Store) PutRegion(r Region) error {
	if err := validRegionID(r.ID); err != nil {
		return err
	}
	if !r.Rule.Known() {
		return fmt.Errorf("worldstore: регион %s: не задано правило подробности (радиус уровня 0 %v, последний уровень %d)",
			r.ID, r.Rule.Level0RadiusM, r.Rule.MaxLevel)
	}
	_, err := s.db.Exec(`
		INSERT INTO regions (id, frame, provenance, redistributable, epoch, level0_radius_m, max_level)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			frame = excluded.frame,
			provenance = excluded.provenance,
			redistributable = excluded.redistributable,
			epoch = excluded.epoch,
			level0_radius_m = excluded.level0_radius_m,
			max_level = excluded.max_level`,
		r.ID, r.Frame, r.Provenance, boolToInt(r.Redistributable), r.Epoch,
		r.Rule.Level0RadiusM, r.Rule.MaxLevel)
	if err != nil {
		return fmt.Errorf("worldstore: запись региона %s: %w", r.ID, err)
	}
	return nil
}

// GetRegion читает регион. Отсутствие — не ошибка.
//
// Регион с НЕИЗВЕСТНЫМ правилом (база прежней сборки) читается как есть и
// ошибкой здесь не считается: хранилище сообщает, что записано, а решает по
// этому тот, кто собирается мир отдавать (worldgen.sameRule). Отказ отсюда
// сделал бы неоткрываемой и ту базу, которую как раз собираются пересоздать.
func (s *Store) GetRegion(id string) (Region, bool, error) {
	var r Region
	var redist int
	err := s.db.QueryRow(
		`SELECT id, frame, provenance, redistributable, epoch, level0_radius_m, max_level
		 FROM regions WHERE id = ?`, id,
	).Scan(&r.ID, &r.Frame, &r.Provenance, &redist, &r.Epoch, &r.Rule.Level0RadiusM, &r.Rule.MaxLevel)
	if errors.Is(err, sql.ErrNoRows) {
		return Region{}, false, nil
	}
	if err != nil {
		return Region{}, false, fmt.Errorf("worldstore: чтение региона %s: %w", id, err)
	}
	r.Redistributable = redist != 0
	return r, true, nil
}

// ListRegions перечисляет регионы по возрастанию идентификатора.
//
// Порядок задан ORDER BY, а не оставлен на усмотрение движка: список едет на
// экран выбора, и регионы, меняющиеся местами от запроса к запросу, выглядели бы
// как изменившийся мир. SQLite без ORDER BY порядка не обещает.
//
// Отдаётся ВЕСЬ список, без страниц. Это осознанный предел: регионов на сервере
// сегодня единицы, и страницы понадобятся вместе с тем, кто их заведёт тысячами.
func (s *Store) ListRegions() ([]Region, error) {
	rows, err := s.db.Query(
		`SELECT id, frame, provenance, redistributable, epoch, level0_radius_m, max_level
		 FROM regions ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("worldstore: перечисление регионов: %w", err)
	}
	defer rows.Close()
	var out []Region
	for rows.Next() {
		var r Region
		var redist int
		if err := rows.Scan(&r.ID, &r.Frame, &r.Provenance, &redist, &r.Epoch,
			&r.Rule.Level0RadiusM, &r.Rule.MaxLevel); err != nil {
			return nil, fmt.Errorf("worldstore: перечисление регионов: %w", err)
		}
		r.Redistributable = redist != 0
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("worldstore: перечисление регионов: %w", err)
	}
	return out, nil
}

// Chunk — статический ярус чанка.
//
// BaseZmm — опорная высота в целых миллиметрах: в базе float не хранится, иначе
// сравнение и хеш начали бы зависеть от представления.
type Chunk struct {
	Address  chunk.Address
	Revision int64
	Hash     string
	BaseZmm  int64
	Heights  []int16
	// Cover — блоб покрова, chunk.CoverBytes байт, либо nil.
	//
	// nil значит «у карты нет рецепта покрова», а не «покров пустой»: ресурс
	// покрова тогда не существует вовсе, и клиент об этом узнаёт кодом ответа, а
	// не разбором нулей.
	Cover []byte
	// Forest — битовая карта занятости, chunk.ForestBytes байт, либо nil.
	// nil на уровнях выше нулевого — не пропуск, а форма: лес существует только
	// там, где его рубят (chunk.ForestLevel).
	Forest []byte
}

// PutChunk записывает статический ярус.
//
// Хеш считается ЗДЕСЬ, от блоба и базы, а не принимается снаружи: он служит
// ETag'ом, и вычисленный вызывающим хеш рано или поздно разошёлся бы с телом.
func (s *Store) PutChunk(c Chunk) error {
	blob, err := chunk.EncodeHeights(c.Heights)
	if err != nil {
		return err
	}
	if c.Cover != nil && len(c.Cover) != chunk.CoverBytes {
		return fmt.Errorf("worldstore: покров %d байт, ожидалось %d", len(c.Cover), chunk.CoverBytes)
	}
	// Версия хеша поднята с v1 до v2, и это не косметика: покров вошёл в
	// содержимое чанка, а хеш служит ETag'ом. Оставь v1 — и чанк, у которого
	// изменился только покров, отдался бы клиенту как неизменённый.
	if c.Forest != nil && len(c.Forest) != chunk.ForestBytes {
		return fmt.Errorf("worldstore: лес %d байт, ожидалось %d", len(c.Forest), chunk.ForestBytes)
	}
	// v3: лес вошёл в содержимое чанка. Версия хеша поднимается вместе с
	// КАЖДЫМ слоем — иначе чанк, у которого изменился только новый слой,
	// отдался бы клиенту как неизменённый.
	h := sha256.New()
	fmt.Fprintf(h, "v3|%d|", c.BaseZmm)
	h.Write(blob)
	h.Write(c.Cover)
	h.Write(c.Forest)
	hash := hex.EncodeToString(h.Sum(nil))

	_, err = s.db.Exec(`
		INSERT INTO chunks (region, level, cx, cz, revision, hash, base_z_mm, heights, cover, forest)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(region, level, cx, cz) DO UPDATE SET
			revision = excluded.revision,
			hash = excluded.hash,
			base_z_mm = excluded.base_z_mm,
			heights = excluded.heights,
			cover = excluded.cover,
			forest = excluded.forest`,
		c.Address.Region, c.Address.Level, c.Address.CX, c.Address.CZ,
		c.Revision, hash, c.BaseZmm, blob, c.Cover, c.Forest)
	if err != nil {
		return fmt.Errorf("worldstore: запись чанка %s/%d/%d/%d: %w",
			c.Address.Region, c.Address.Level, c.Address.CX, c.Address.CZ, err)
	}
	return nil
}

// GetChunk читает статический ярус.
//
// ОТСУТСТВИЕ ЧАНКА — НЕ ОШИБКА. Разреженность есть свойство хранилища, а не
// сбой: чанк не хранится там, где ничего нет, и спросивший пустоту получает
// «здесь ничего нет», а не отказ (`world-storage` §5, контракт чанков §6).
func (s *Store) GetChunk(a chunk.Address) (Chunk, bool, error) {
	var c Chunk
	c.Address = a
	var blob []byte
	err := s.db.QueryRow(`
		SELECT revision, hash, base_z_mm, heights, cover, forest FROM chunks
		WHERE region = ? AND level = ? AND cx = ? AND cz = ?`,
		a.Region, a.Level, a.CX, a.CZ,
	).Scan(&c.Revision, &c.Hash, &c.BaseZmm, &blob, &c.Cover, &c.Forest)
	if errors.Is(err, sql.ErrNoRows) {
		return Chunk{}, false, nil
	}
	if err != nil {
		return Chunk{}, false, fmt.Errorf("worldstore: чтение чанка %s/%d/%d/%d: %w",
			a.Region, a.Level, a.CX, a.CZ, err)
	}
	c.Heights, err = chunk.DecodeHeights(blob)
	if err != nil {
		return Chunk{}, false, err
	}
	return c, true, nil
}

// CountChunks — сколько чанков лежит у региона на уровне.
func (s *Store) CountChunks(region string, level int) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT count(*) FROM chunks WHERE region = ? AND level = ?`, region, level).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("worldstore: счёт чанков %s: %w", region, err)
	}
	return n, nil
}

func validRegionID(id string) error {
	if id == "" {
		return fmt.Errorf("worldstore: пустой идентификатор региона")
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
