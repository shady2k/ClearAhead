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
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"

	_ "modernc.org/sqlite"
)

// schema — форма базы.
//
// Ревизия и хеш живут У ЧАНКА, а не у карты: на регионе размером с край любая
// правка обесценивала бы кэш всего мира. Манифест региона несёт эпоху, а не
// поштучный список ревизий (`world-storage` §4).
//
// Ключ чанка несёт world_version (спека §6.3): строка адресуется (место,
// версия), а не местом. Из этого ключа и растёт защита от устаревшего писателя
// — запрос, начавший счёт до правки и закончивший после, допишет СВОЮ версию и
// физически не сможет затереть новую: это ДРУГАЯ строка. CAS не нужен — он
// решал бы уже решённую задачу дороже (разбор в §6.3 спеки).
//
// projection_heads — голова проекций региона: какая версия мира текущая и из
// чего она собрана (журнал, сеть, рецепт). Минимальная форма §6.3 спеки; Merkle,
// индексы и дедупликация отложены до замеренного потребителя.
//
// network_versions — тело сети ПО ВЕРСИИ МИРА: адрес /worlds/{v}/network обязан
// отдавать одно и то же тело навсегда, иначе immutable на нём лжёт. Копия тела
// на каждую публикацию, которая сеть меняет — это и есть отсутствие
// дедупликации между версиями, названное в §6.3 спеки отложенным.
const schema = `
CREATE TABLE IF NOT EXISTS regions (
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
) STRICT;

CREATE TABLE IF NOT EXISTS chunks (
    region        TEXT    NOT NULL REFERENCES regions(id),
    level         INTEGER NOT NULL,
    cx            INTEGER NOT NULL,
    cz            INTEGER NOT NULL,
    world_version INTEGER NOT NULL,
    revision      INTEGER NOT NULL,
    hash          TEXT    NOT NULL,
    base_z_mm     INTEGER NOT NULL,
    heights       BLOB    NOT NULL,
    cover         BLOB,
    forest        BLOB,
    PRIMARY KEY (region, level, cx, cz, world_version)
) STRICT;

CREATE TABLE IF NOT EXISTS projection_heads (
    region             TEXT PRIMARY KEY REFERENCES regions(id),
    world_version      INTEGER NOT NULL,
    source_journal_seq INTEGER NOT NULL,
    network_version    INTEGER NOT NULL,
    region_recipe_hash TEXT    NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS network_versions (
    region        TEXT    NOT NULL REFERENCES regions(id),
    world_version INTEGER NOT NULL,
    body          BLOB    NOT NULL,
    hash          TEXT    NOT NULL,
    PRIMARY KEY (region, world_version)
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
	`ALTER TABLE regions ADD COLUMN domain_min_x REAL NOT NULL DEFAULT 0`,
	`ALTER TABLE regions ADD COLUMN domain_min_z REAL NOT NULL DEFAULT 0`,
	`ALTER TABLE regions ADD COLUMN domain_max_x REAL NOT NULL DEFAULT 0`,
	`ALTER TABLE regions ADD COLUMN domain_max_z REAL NOT NULL DEFAULT 0`,
}

// rebuildChunkVersionKey — перестройка таблицы чанков: первичный ключ получает
// world_version (строка адресуется местом и версией, спека §6.3). ADD COLUMN
// добавить колонку в первичный ключ не может, поэтому таблица пересоздаётся.
//
// В ОТЛИЧИЕ от прочих миграций эта НЕ идемпотентна по построению: перестройка
// на каждой старте копировала бы всю таблицу ради ничего, а на большом регионе
// (76 630 чанков на 2 000 км оси, спека §8.1) — это секунды каждой загрузки.
// Поэтому она гонится один раз, под охраной проверки колонки (chunksHaveVersion
// в Open): существующие строки получают версию 1 — мир, засеянный прежней
// сборкой, это и есть первая публикация.
const rebuildChunkVersionKey = `
CREATE TABLE chunks_versioned (
    region        TEXT    NOT NULL REFERENCES regions(id),
    level         INTEGER NOT NULL,
    cx            INTEGER NOT NULL,
    cz            INTEGER NOT NULL,
    world_version INTEGER NOT NULL,
    revision      INTEGER NOT NULL,
    hash          TEXT    NOT NULL,
    base_z_mm     INTEGER NOT NULL,
    heights       BLOB    NOT NULL,
    cover         BLOB,
    forest        BLOB,
    PRIMARY KEY (region, level, cx, cz, world_version)
) STRICT;
INSERT INTO chunks_versioned
    SELECT region, level, cx, cz, 1, revision, hash, base_z_mm, heights, cover, forest
    FROM chunks;
DROP TABLE chunks;
ALTER TABLE chunks_versioned RENAME TO chunks;
`

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
	// Перестройка ключа чанков — под охраной проверки колонки, а не по
	// подстроке ошибки: она разрушительна (DROP TABLE) и не должна гоняться
	// при каждом открытии (см. rebuildChunkVersionKey). Схема выше уже создала
	// таблицу новой формы на чистой базе, и здесь колонка есть — миграция
	// молча пропускается.
	has, err := chunksHaveWorldVersion(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("worldstore: проверка ключа чанков: %w", err)
	}
	if !has {
		if _, err := db.Exec(rebuildChunkVersionKey); err != nil {
			db.Close()
			return nil, fmt.Errorf("worldstore: перестройка ключа чанков: %w", err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// chunksHaveWorldVersion отвечает, несёт ли таблица чанков колонку версии —
// единственный признак, по которому перестройку ключа можно не повторять.
func chunksHaveWorldVersion(db *sql.DB) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(chunks)")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == "world_version" {
			return true, nil
		}
	}
	return false, rows.Err()
}

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
	// Domain — прямоугольник домена, КОТОРЫМ ЭТОТ РЕГИОН ОБЪЯВЛЕН.
	//
	// Хранится, а не выводится из карты при каждом чтении, по той же причине,
	// что и правило выше: карта на диске сменится, а мир в базе останется
	// прежним. Клиент читает домен манифестом региона — это ответ на вопрос
	// «где мир кончается», — и манифест обязан называть домен ХРАНИЛИЩА, а не
	// файла карты. Разойтись им не дают на старте (worldgen.sameDomain).
	Domain mapfmt.Domain
}

// PutRegion записывает или обновляет регион.
//
// Регион без правила подробности или без домена не записывается: он был бы
// регионом, про чанки которого нельзя сказать, ни откуда они взялись, ни до
// каких границ мир существует.
func (s *Store) PutRegion(r Region) error {
	if err := validRegionID(r.ID); err != nil {
		return err
	}
	if !r.Rule.Known() {
		return fmt.Errorf("worldstore: регион %s: не задано правило подробности (радиус уровня 0 %v, последний уровень %d)",
			r.ID, r.Rule.Level0RadiusM, r.Rule.MaxLevel)
	}
	if !r.Domain.Known() {
		return fmt.Errorf("worldstore: регион %s: не задан домен (x от %v до %v, z от %v до %v)",
			r.ID, r.Domain.MinX, r.Domain.MaxX, r.Domain.MinZ, r.Domain.MaxZ)
	}
	_, err := s.db.Exec(`
		INSERT INTO regions (id, frame, provenance, redistributable, epoch,
			level0_radius_m, max_level, domain_min_x, domain_min_z, domain_max_x, domain_max_z)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			frame = excluded.frame,
			provenance = excluded.provenance,
			redistributable = excluded.redistributable,
			epoch = excluded.epoch,
			level0_radius_m = excluded.level0_radius_m,
			max_level = excluded.max_level,
			domain_min_x = excluded.domain_min_x,
			domain_min_z = excluded.domain_min_z,
			domain_max_x = excluded.domain_max_x,
			domain_max_z = excluded.domain_max_z`,
		r.ID, r.Frame, r.Provenance, boolToInt(r.Redistributable), r.Epoch,
		r.Rule.Level0RadiusM, r.Rule.MaxLevel,
		r.Domain.MinX, r.Domain.MinZ, r.Domain.MaxX, r.Domain.MaxZ)
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
		`SELECT id, frame, provenance, redistributable, epoch, level0_radius_m, max_level,
		        domain_min_x, domain_min_z, domain_max_x, domain_max_z
		 FROM regions WHERE id = ?`, id,
	).Scan(&r.ID, &r.Frame, &r.Provenance, &redist, &r.Epoch, &r.Rule.Level0RadiusM, &r.Rule.MaxLevel,
		&r.Domain.MinX, &r.Domain.MinZ, &r.Domain.MaxX, &r.Domain.MaxZ)
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
		`SELECT id, frame, provenance, redistributable, epoch, level0_radius_m, max_level,
		        domain_min_x, domain_min_z, domain_max_x, domain_max_z
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
			&r.Rule.Level0RadiusM, &r.Rule.MaxLevel,
			&r.Domain.MinX, &r.Domain.MinZ, &r.Domain.MaxX, &r.Domain.MaxZ); err != nil {
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
	Address chunk.Address
	// WorldVersion — версия мира, К КОТОРОЙ ПРИНАДЛЕЖИТ СТРОКА: она входит в
	// ключ, и две версии одного места лежат рядом, не затирая друг друга
	// (спека §6.3). Ноль — неверный ключ: запись обязана называть версию.
	WorldVersion int64
	// Revision — ревизия КАРТЫ, из которой строка посчитана. Отделена от
	// WorldVersion нарочно: ревизия растёт от авторской правки файла, версия —
	// от публикации проекций, и смешивать их значило бы получить «новый корень
	// мира на каждое движение машиниста» (спека §6.3).
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

func (s *Store) PutChunk(c Chunk) error {
	return upsertChunk(s.db, c)
}

// PutChunks записывает пачку чанков ОДНОЙ транзакцией.
//
// Группа согласованности (project.Group) публикуется атомарно: читатель не
// видит половину группы — шовный инвариант держится тем, что оба соседа,
// делящие граничный ряд отсчётов, попадают в одну транзакцию. Пустая пачка —
// согласованный no-op, а не ошибка: замыкание может дать группу, все адреса
// которой уже записаны другой группой.
//
// Один чанк пачки, не прошедший проверку, откатывает ВСЮ пачку: частичная
// группа хуже отсутствующей — она выглядела бы опубликованной.
func (s *Store) PutChunks(cs []Chunk) error {
	if len(cs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("worldstore: пачка чанков: %w", err)
	}
	defer tx.Rollback()
	for _, c := range cs {
		if err := upsertChunk(tx, c); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("worldstore: пачка чанков: %w", err)
	}
	return nil
}

// execer — то, на чём можно выполнить запись: соединение или транзакция.
// Узкий нарочно: общей записи нужен только Exec, и интерфейс шире развёл бы
// реализацию с потребителем.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// upsertChunk — одна строка записи чанка: хеш от блоба и базы и безусловный
// ON CONFLICT DO UPDATE. Общий для одиночной записи (PutChunk) и пачки
// (PutChunks), чтобы хеш и форма строки не разошлись между двумя путями.
//
// Конфликт разрешается В ПРЕДЕЛАХ ВЕРСИИ: ключ несёт world_version, и запись
// той же версии идемпотентна (повторный счёт даёт те же байты), а запись
// ДРУГОЙ версии — это другая строка, и она ничего не затирает. Защита от
// устаревшего писателя держится этим, а не транзакцией сравнения (спека §6.3).
func upsertChunk(e execer, c Chunk) error {
	if c.WorldVersion <= 0 {
		return fmt.Errorf("worldstore: чанк %s/%d/%d/%d: не названа версия мира",
			c.Address.Region, c.Address.Level, c.Address.CX, c.Address.CZ)
	}
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

	_, err = e.Exec(`
		INSERT INTO chunks (region, level, cx, cz, world_version, revision, hash, base_z_mm, heights, cover, forest)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(region, level, cx, cz, world_version) DO UPDATE SET
			revision = excluded.revision,
			hash = excluded.hash,
			base_z_mm = excluded.base_z_mm,
			heights = excluded.heights,
			cover = excluded.cover,
			forest = excluded.forest`,
		c.Address.Region, c.Address.Level, c.Address.CX, c.Address.CZ,
		c.WorldVersion, c.Revision, hash, c.BaseZmm, blob, c.Cover, c.Forest)
	if err != nil {
		return fmt.Errorf("worldstore: запись чанка %s/%d/%d/%d: %w",
			c.Address.Region, c.Address.Level, c.Address.CX, c.Address.CZ, err)
	}
	return nil
}

// GetChunk читает статический ярус ПО СОСТОЯНИЮ НА ВЕРСИЮ: строку (место,
// версия) ровно версии atOrBefore, а при отсутствии — ПОСЛЕДНЮЮ записанную
// версию не старше. Оба смысла нужны и не сливаются: /worlds/{v}/… отдаёт
// замороженный мир (строки старше v для адреса, НЕ вошедшего в публикацию v,
// — это ровно его содержимое: замыкание материализует изменившиеся клетки,
// а неизменившиеся сохраняют прежние байты), а неверсионный адрес отдаёт
// текущий мир (последнюю публикацию).
//
// ОТСУТСТВИЕ ЧАНКА — НЕ ОШИБКА. Разреженность есть свойство хранилища, а не
// сбой: чанк не хранится там, где ничего нет, и спросивший пустоту получает
// «здесь ничего нет», а не отказ (`world-storage` §5, контракт чанков §6).
func (s *Store) GetChunk(a chunk.Address, atOrBefore int64) (Chunk, bool, error) {
	var c Chunk
	c.Address = a
	var blob []byte
	err := s.db.QueryRow(`
		SELECT world_version, revision, hash, base_z_mm, heights, cover, forest FROM chunks
		WHERE region = ? AND level = ? AND cx = ? AND cz = ? AND world_version <= ?
		ORDER BY world_version DESC
		LIMIT 1`,
		a.Region, a.Level, a.CX, a.CZ, atOrBefore,
	).Scan(&c.WorldVersion, &c.Revision, &c.Hash, &c.BaseZmm, &blob, &c.Cover, &c.Forest)
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

// CountChunks — сколько строк лежит у региона на уровне. С версионированным
// ключом одна клетка может нести несколько строк (по строке на публикацию),
// и счёт отвечает на вопрос «сколько лежит всего», а не «сколько клеток»:
// выборки прогрева и тесты считают ЗАПИСИ, и менять им единицу счёта без
// запроса нельзя.
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

// ProjectionHead — голова проекций региона (спека §6.3, минимальная форма).
//
// Четыре поля отвечают на четыре разных вопроса, и смешивать их нельзя:
//
//	WorldVersion      — какая версия мира текущая (монотонная, растёт от
//	                    ПУБЛИКАЦИИ пространственных проекций);
//	SourceJournalSeq  — до какой команды журнала построено (растёт от ЛЮБОЙ
//	                    команды, включая движение машиниста, и версии мира НЕ
//	                    двигает — разбор в §6.3 спеки);
//	NetworkVersion    — какая версия сети внутри этой публикации (растёт
//	                    только когда публикация перекомпилировала сеть);
//	RegionRecipeHash  — хеш рецепта региона (зерно, генератор, реки, покров):
//	                    рецепт неизменен, и по нему клиент узнаёт «земля та же».
type ProjectionHead struct {
	WorldVersion     int64
	SourceJournalSeq int64
	NetworkVersion   int64
	RegionRecipeHash string
}

// GetProjectionHead читает голову проекций региона. Отсутствие — не ошибка:
// база прежней сборки головы не несёт, и решает по отсутствию тот, кто
// собирается публиковать (Bootstrap дописывает голову при старте).
func (s *Store) GetProjectionHead(region string) (ProjectionHead, bool, error) {
	var h ProjectionHead
	err := s.db.QueryRow(
		`SELECT world_version, source_journal_seq, network_version, region_recipe_hash
		 FROM projection_heads WHERE region = ?`, region,
	).Scan(&h.WorldVersion, &h.SourceJournalSeq, &h.NetworkVersion, &h.RegionRecipeHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectionHead{}, false, nil
	}
	if err != nil {
		return ProjectionHead{}, false, fmt.Errorf("worldstore: голова проекций %s: %w", region, err)
	}
	return h, true, nil
}

// PutProjectionHead пишет голову проекций целиком.
//
// Отдельный метод существует ради ЖУРНАЛА: движение машиниста продвигает
// SourceJournalSeq, не трогая версию мира, и эта запись — единственный путь,
// которым журнал дойдёт до головы до появления строительных команд.
func (s *Store) PutProjectionHead(region string, h ProjectionHead) error {
	if h.WorldVersion <= 0 {
		return fmt.Errorf("worldstore: голова проекций %s: не названа версия мира", region)
	}
	// Монотонность — инвариант головы, а не вежливость: голова, откатившаяся
	// со второй версии на первую, заставила бы неверсионный адрес отдавать
	// старую землю под именем текущей, а откат source_journal_seq при равной
	// версии солгал бы «до какой команды построено» (бида sqym.20). Проверка
	// живёт В УСЛОВИИ ОБНОВЛЕНИЯ, а не в прочитанной заранее голове: два
	// конкурирующих продвижения журнала не должны успеть оба пройти проверку
	// и записать младшее значение последним. Условие пропускает рост версии
	// (публикация) и при равной версии — неоткатывающийся журнал
	// (продвижение), и режет откат версии и откат журнала.
	res, err := s.db.Exec(`
		INSERT INTO projection_heads (region, world_version, source_journal_seq, network_version, region_recipe_hash)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(region) DO UPDATE SET
			world_version = excluded.world_version,
			source_journal_seq = excluded.source_journal_seq,
			network_version = excluded.network_version,
			region_recipe_hash = excluded.region_recipe_hash
		WHERE projection_heads.world_version < excluded.world_version
		   OR (projection_heads.world_version = excluded.world_version
		       AND projection_heads.source_journal_seq <= excluded.source_journal_seq)`,
		region, h.WorldVersion, h.SourceJournalSeq, h.NetworkVersion, h.RegionRecipeHash)
	if err != nil {
		return fmt.Errorf("worldstore: запись головы проекций %s: %w", region, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("worldstore: голова проекций %s: запись (версия %d, журнал %d) откатывает текущую голову",
			region, h.WorldVersion, h.SourceJournalSeq)
	}
	return nil
}

// Seed заводит регион С ГОЛОВОЙ ПРОЕКЦИЙ И ПЕРВОЙ СЕТЬЮ одной транзакцией.
//
// Три записи — один факт рождения мира: регион без головы не публикуем,
// голова без сети не отдаёт /worlds/1/network, а сеть без региона упала бы на
// внешнем ключе.
//
// ИДЕМПОТЕНТНА НАРОЧНО: бутстрап зовёт её и при рождении мира, и при старте
// на базе прежней сборки (регион есть, головы нет — перестройка ключа дала
// чанкам версию 1, и голова обязана назвать ту же). Существующие строки не
// перезаписываются: живой мир с головой версии 5 не откатится к версии 1.
func (s *Store) Seed(region Region, head ProjectionHead, network []byte) error {
	if head.WorldVersion <= 0 {
		return fmt.Errorf("worldstore: засев %s: не названа версия мира", region.ID)
	}
	if err := validRegionID(region.ID); err != nil {
		return err
	}
	if !region.Rule.Known() {
		return fmt.Errorf("worldstore: засев %s: не задано правило подробности", region.ID)
	}
	if !region.Domain.Known() {
		return fmt.Errorf("worldstore: засев %s: не задан домен", region.ID)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("worldstore: засев %s: %w", region.ID, err)
	}
	defer tx.Rollback()
	// DO NOTHING вместо DO UPDATE: регион, уже заведённый прежним стартом,
	// несёт свою геопривязку и происхождение, и переписывать их при
	// повторном бутстрапе — молчаливо стирать историю.
	_, err = tx.Exec(`
		INSERT INTO regions (id, frame, provenance, redistributable, epoch,
			level0_radius_m, max_level, domain_min_x, domain_min_z, domain_max_x, domain_max_z)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		region.ID, region.Frame, region.Provenance, boolToInt(region.Redistributable), region.Epoch,
		region.Rule.Level0RadiusM, region.Rule.MaxLevel,
		region.Domain.MinX, region.Domain.MinZ, region.Domain.MaxX, region.Domain.MaxZ)
	if err != nil {
		return fmt.Errorf("worldstore: засев %s: регион: %w", region.ID, err)
	}
	_, err = tx.Exec(`
		INSERT INTO projection_heads (region, world_version, source_journal_seq, network_version, region_recipe_hash)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(region) DO NOTHING`,
		region.ID, head.WorldVersion, head.SourceJournalSeq, head.NetworkVersion, head.RegionRecipeHash)
	if err != nil {
		return fmt.Errorf("worldstore: засев %s: голова: %w", region.ID, err)
	}
	if network != nil {
		if err := insertNetworkVersion(tx, region.ID, head.WorldVersion, network); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("worldstore: засев %s: %w", region.ID, err)
	}
	return nil
}

// Publish публикует группу согласованности: строки чанков версии v, при
// наличии сети и обновлённую голову — ОДНОЙ транзакцией (спека §6.3 «мир
// публикуется одной версией»). Читатель не видит промежуточного состояния
// «сеть новая, земля старая»: версия, названная головой, появляется целиком
// или не появляется вовсе.
//
// network == nil означает «сеть в эту публикацию не входит»: тело версии v
// берётся из последней записанной версии — ровно то, что и должно быть,
// потому что замыкание пересобирает сеть только когда изменились её исходники.
// network_version при этом не двигается: версия сети — счётчик ПЕРЕКОМПИЛЯЦИЙ,
// а не публикаций.
//
// Пустая публикация (ни строк, ни сети) — отказ, а не тихий шаг версии: версия
// мира обязана что-то публиковать, иначе счётчик растёт без причины и клиент
// перезагружает неизменившийся мир.
func (s *Store) Publish(region string, rows []Chunk, network []byte, journalSeq int64) (ProjectionHead, error) {
	if len(rows) == 0 && network == nil {
		return ProjectionHead{}, fmt.Errorf("worldstore: публикация %s: пустая публикация (ни строк, ни сети)", region)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return ProjectionHead{}, fmt.Errorf("worldstore: публикация %s: %w", region, err)
	}
	defer tx.Rollback()
	// Номер версии выдаёт АТОМАРНЫЙ СЧЁТЧИК в первой же записи транзакции
	// (бида sqym.17). Прежний код читал голову ДО db.Begin и прибавлял единицу
	// снаружи: два одновременных публикатора выбирали ОДИН номер, и второй
	// безусловным DO UPDATE затирал строки первого — содержимое под
	// immutable-адресом сменилось бы. Инкремент в самом UPDATE и есть выдача:
	// оператор читает и пишет счётчик одним движением, окна «прочитать —
	// изменить — записать» нет, и два публикатора физически не могут получить
	// один номер. Откат транзакции откатывает и выдачу: неудачная публикация
	// номера не сжигает.
	//
	// Отвергнута условная форма (UPDATE ... WHERE world_version = ? с проверкой
	// числа затронутых строк): она оставляет охранённое, но реальное окно
	// между чтением и записью и корректна только при старте транзакции
	// писателем — под deferred BEGIN в WAL коммит конкурента в это окно дал бы
	// SQLITE_BUSY_SNAPSHOT, который busy_timeout не ждёт.
	//
	// Изоляции хватает без изменений соединения: WAL, deferred BEGIN,
	// busy_timeout 5000 мс. Счётчик — ПЕРВЫЙ оператор транзакции, до любого
	// SELECT: писатель берёт блокировку записи ДО установки снапшота,
	// проигравший гонку ждёт её в busy_timeout, и его счётчик инкрементится
	// от уже свежего снапшота. SQLITE_BUSY_SNAPSHOT (обновление снапшота после
	// чтения) здесь невозможен — читать до выдачи нечего.
	//
	// source_journal_seq пишется ровно переданным значением, без охраны от
	// отката: откат журнала при РАВНОЙ версии невозможен по построению (версия
	// строго растёт), а монотонность журнала между публикациями — инвариант
	// зовущей стороны (журнал только дописывается), и охранять его здесь —
	// молчаливо чинить чужой контракт.
	netDelta := int64(0)
	if network != nil {
		netDelta = 1
	}
	var head ProjectionHead
	err = tx.QueryRow(`
		UPDATE projection_heads
		SET world_version = world_version + 1,
			source_journal_seq = ?,
			network_version = network_version + ?
		WHERE region = ?
		RETURNING world_version, source_journal_seq, network_version, region_recipe_hash`,
		journalSeq, netDelta, region,
	).Scan(&head.WorldVersion, &head.SourceJournalSeq, &head.NetworkVersion, &head.RegionRecipeHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectionHead{}, fmt.Errorf("worldstore: публикация %s: голова проекций не заведена (бутстрап не выполнен)", region)
	}
	if err != nil {
		return ProjectionHead{}, fmt.Errorf("worldstore: публикация %s: выдача номера версии: %w", region, err)
	}
	for _, c := range rows {
		c.WorldVersion = head.WorldVersion
		if err := upsertChunk(tx, c); err != nil {
			return ProjectionHead{}, err
		}
	}
	if network != nil {
		if err := insertNetworkVersion(tx, region, head.WorldVersion, network); err != nil {
			return ProjectionHead{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ProjectionHead{}, fmt.Errorf("worldstore: публикация %s: %w", region, err)
	}
	return head, nil
}

// insertNetworkVersion — строка тела сети: хеш считается ЗДЕСЬ, от тех байт,
// что лягут в базу, и служит ETag'ом версионного адреса. Общая для Seed и
// Publish, чтобы два пути записи не разошлись по хешу.
func insertNetworkVersion(e execer, region string, worldVersion int64, body []byte) error {
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	// ON CONFLICT DO NOTHING: версия мира — строгий ключ, и повтор той же
	// версии означает повторную запись ТОГО ЖЕ тела (выводы детерминированы).
	// Упасть на уникальности значило бы сделать идемпотентный бутстрап
	// неидемпотентным.
	if _, err := e.Exec(`
		INSERT INTO network_versions (region, world_version, body, hash)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(region, world_version) DO NOTHING`,
		region, worldVersion, body, hash); err != nil {
		return fmt.Errorf("worldstore: сеть %s версии %d: %w", region, worldVersion, err)
	}
	return nil
}

// GetNetwork версии мира: тело и хеш сети ПО СОСТОЯНИЮ НА ВЕРСИЮ — строка
// ровно версии atOrBefore, а при отсутствии последняя записанная не старше
// (тот же смысл, что у GetChunk: публикация без сети сохраняет прежнее тело).
// Отсутствие — не ошибка: база без засева сети ещё не имеет.
func (s *Store) GetNetwork(region string, atOrBefore int64) (body, hash string, ok bool, err error) {
	var b []byte
	err = s.db.QueryRow(`
		SELECT body, hash FROM network_versions
		WHERE region = ? AND world_version <= ?
		ORDER BY world_version DESC
		LIMIT 1`,
		region, atOrBefore,
	).Scan(&b, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("worldstore: сеть %s: %w", region, err)
	}
	return string(b), hash, true, nil
}

// RecipeHash — хеш рецепта региона: блок terrain карты целиком (зерно,
// генератор высот, земляные работы, охват, домен, покров). Рецепт неизменен —
// карта его не правит, — и по хешу клиент узнаёт «земля та же», не сравнивая
// байты мира. JSON-сериализация детерминирована (encoding/json сортирует ключи
// карт), и домен входит в рецепт: изменение границ мира — изменение мира.
func RecipeHash(t mapfmt.Terrain) (string, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("worldstore: хеш рецепта: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Region возвращает идентификатор региона, которому принадлежит голова.
