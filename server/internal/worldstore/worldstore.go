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
    epoch           INTEGER NOT NULL DEFAULT 1
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
var migrations = []string{
	`ALTER TABLE chunks ADD COLUMN cover BLOB`,
	`ALTER TABLE chunks ADD COLUMN forest BLOB`,
}

// Store — открытая база мира.
type Store struct{ db *sql.DB }

// Open открывает или создаёт базу по пути.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("worldstore: открытие %s: %w", path, err)
	}
	// Внешние ключи в SQLite выключены по умолчанию, и ссылка чанка на
	// несуществующий регион прошла бы молча.
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("worldstore: включение внешних ключей: %w", err)
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
}

// PutRegion записывает или обновляет регион.
func (s *Store) PutRegion(r Region) error {
	if err := validRegionID(r.ID); err != nil {
		return err
	}
	_, err := s.db.Exec(`
		INSERT INTO regions (id, frame, provenance, redistributable, epoch)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			frame = excluded.frame,
			provenance = excluded.provenance,
			redistributable = excluded.redistributable,
			epoch = excluded.epoch`,
		r.ID, r.Frame, r.Provenance, boolToInt(r.Redistributable), r.Epoch)
	if err != nil {
		return fmt.Errorf("worldstore: запись региона %s: %w", r.ID, err)
	}
	return nil
}

// GetRegion читает регион. Отсутствие — не ошибка.
func (s *Store) GetRegion(id string) (Region, bool, error) {
	var r Region
	var redist int
	err := s.db.QueryRow(
		`SELECT id, frame, provenance, redistributable, epoch FROM regions WHERE id = ?`, id,
	).Scan(&r.ID, &r.Frame, &r.Provenance, &redist, &r.Epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return Region{}, false, nil
	}
	if err != nil {
		return Region{}, false, fmt.Errorf("worldstore: чтение региона %s: %w", id, err)
	}
	r.Redistributable = redist != 0
	return r, true, nil
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
