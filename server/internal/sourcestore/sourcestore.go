// Package sourcestore — хранилище АВТОРИТЕТНЫХ ИСХОДНИКОВ партии.
//
// # Зачем отдельная база
//
// Контракт (sources-compilers-projections.md §1) разводит два класса: исходник
// журналируется и переживает всё, проекция производна и сносится когда угодно.
// В cmd/clearahead это разведение уже стоит сторожем assertProjectionsOnly:
// база проекций (worldstore) обязана нести ТОЛЬКО проекции, и -reseed сносит
// её целиком. Хранилища, которое сторож подразумевает — куда исходники
// ЛОЖАТСЯ, чтобы их можно было не сносить, — до sqym.18 не существовало:
// правки высот жили в памяти edit.Service и умирали вместе с процессом.
// Этот пакет и есть то хранилище: отдельный файл SQLite рядом с базой
// проекций, который -reseed не касается вовсе.
//
// # Форма — состояние-множество, а не журнал-аппенд
//
// Исходники партии — МНОЖЕСТВА, а не списки событий: рубка дважды — одна
// рубка, повторная доставка команды не меняет результат (семантика
// vegetation.Sources). Поэтому таблицы несут СОСТОЯНИЕ множества, а не
// аппенд-журнал, и каждая запись идемпотентна по своему ключу. Единственный
// аппенд — история коммитов (commits): её порядок — позиция журнала, на
// которую опирается окно конфликта edit.
//
// # Отвергнуто: аппенд-журнал команд (JSONL / таблица команд)
//
// Эпик в идеале хранит команды строителя и переигрывает их (исходник
// журналируется). Прежде, чем сюда легли бы команды, нужен их производитель
// — транспорт команд строителя, шаг 12, вне этого эпика; форма без
// потребителя запрещена. Сегодня авторитетный факт — ЗАКОММИЧЕННОЕ:
// закоммиченная карта, правки высот, вырубка, история коммитов. Оно и
// хранится; откат (переигровка журнала) не нужен — «отмены нет и не будет»
// (решение владельца, edit.go).
//
// # Атомарность
//
// Коммит edit меняет ТРИ множества разом — карту, правки высот и историю
// коммитов — и три записи обязаны лечь одной транзакцией: сбой между ними
// оставил бы закоммиченный мир и его правки на разных поколениях, а это
// ровно та тихая потеря, ради которой пакет существует. Запись идёт через
// Tx (Begin), а не через отдельные методы: вне транзакции эти множества
// писать некому.
package sourcestore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/vegetation"

	_ "modernc.org/sqlite"
)

// schema — форма базы исходников.
//
// committed_map — закоммиченная карта целиком (JSON): путь партии в том виде,
// в каком его принял коммит. Один ряд, id = 1 — карта одна на партию.
//
// grading — правки высот закоммиченного мира: клетка уровня 0 с абсолютной
// отметкой. Уровень в форме отсутствует НАРОЧНО: правки существуют только на
// уровне 0 (terrain.Grading.Validate), и колонка уровня заставила бы форму
// выражать невыразимое.
//
// vegetation_* — источники растительности (вырубка, посадка): те же четыре
// формы, что в vegetation.Sources. Cut и Planted адресуются (чанк, ячейка),
// CutMask — (чанк, биты), Clearing — прямоугольником региона.
//
// commits — история принятых транзакций: затронутые элементы пути и клетки
// правок. seq — позиция в журнале: окно конфликта edit читает коммиты,
// лёгшие после создания макета, по индексу.
const schema = `
CREATE TABLE IF NOT EXISTS committed_map (
    id       INTEGER PRIMARY KEY CHECK (id = 1),
    map_json BLOB NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS grading (
    cx        INTEGER NOT NULL,
    cz        INTEGER NOT NULL,
    height_cm INTEGER NOT NULL,
    PRIMARY KEY (cx, cz)
) STRICT;

CREATE TABLE IF NOT EXISTS vegetation_cuts (
    cx INTEGER NOT NULL, cz INTEGER NOT NULL,
    i  INTEGER NOT NULL, j  INTEGER NOT NULL,
    PRIMARY KEY (cx, cz, i, j)
) STRICT;

CREATE TABLE IF NOT EXISTS vegetation_planted (
    cx INTEGER NOT NULL, cz INTEGER NOT NULL,
    i  INTEGER NOT NULL, j  INTEGER NOT NULL,
    PRIMARY KEY (cx, cz, i, j)
) STRICT;

CREATE TABLE IF NOT EXISTS vegetation_clearings (
    id    INTEGER PRIMARY KEY,
    min_x REAL NOT NULL, min_z REAL NOT NULL,
    max_x REAL NOT NULL, max_z REAL NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS vegetation_cut_masks (
    cx   INTEGER NOT NULL, cz INTEGER NOT NULL,
    bits BLOB NOT NULL,
    PRIMARY KEY (cx, cz)
) STRICT;

CREATE TABLE IF NOT EXISTS commits (
    seq      INTEGER PRIMARY KEY,
    elements TEXT NOT NULL,
    cells    TEXT NOT NULL
) STRICT;
`

// Store — открытая база исходников партии.
type Store struct{ db *sql.DB }

// dsnPragmas — настройки соединений, тот же набор, что у worldstore: WAL
// (читатели не ждут писателя), busy_timeout (конкуренция), foreign_keys.
// Настройки едут в строке подключения, а не Exec'ом, потому что database/sql
// держит пул соединений, а PRAGMA действует на одно.
const dsnPragmas = "?_pragma=foreign_keys(1)" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=journal_mode(WAL)" +
	"&_pragma=synchronous(NORMAL)"

// Open открывает или создаёт базу исходников по пути.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+dsnPragmas)
	if err != nil {
		return nil, fmt.Errorf("sourcestore: открыть базу %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("sourcestore: схема базы %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// execer — то, на чём можно выполнить запись: соединение или транзакция.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// CommitRecord — одна принятая транзакция: затронутые элементы пути и клетки
// правок высот. То, что окно конфликта edit читает из истории коммитов
// (tx.go, svc.commits).
type CommitRecord struct {
	// Elements — идентификаторы затронутых элементов пути.
	Elements []string
	// Cells — затронутые клетки правок высот.
	Cells []GradeCell
}

// GradeCell — адрес клетки правки высот в плане. Уровень в адресе не хранится:
// правки существуют только на уровне 0.
type GradeCell struct {
	CX, CZ int
}

// GetCommittedMap читает закоммиченную карту. Отсутствие — не ошибка: первой
// сессии хранилища ещё нечего хранить, и решает по отсутствию edit
// (NewServiceStored берёт карту-затравку).
func (s *Store) GetCommittedMap() (*mapfmt.Map, bool, error) {
	row := s.db.QueryRow(`SELECT map_json FROM committed_map WHERE id = 1`)
	var blob []byte
	if err := row.Scan(&blob); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("sourcestore: закоммиченная карта: %w", err)
	}
	var m mapfmt.Map
	if err := json.Unmarshal(blob, &m); err != nil {
		return nil, false, fmt.Errorf("sourcestore: закоммиченная карта не читается: %w", err)
	}
	return &m, true, nil
}

func putCommittedMap(e execer, m *mapfmt.Map) error {
	blob, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("sourcestore: закоммиченная карта не сериализуется: %w", err)
	}
	if _, err := e.Exec(`INSERT INTO committed_map (id, map_json) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET map_json = excluded.map_json`, blob); err != nil {
		return fmt.Errorf("sourcestore: закоммиченная карта: %w", err)
	}
	return nil
}

// GetGrading читает правки высот закоммиченного мира. Порядок детерминирован
// (по клетке), чтобы эталонное сравнение не зависело от плана запроса.
func (s *Store) GetGrading() (terrain.Grading, error) {
	rows, err := s.db.Query(`SELECT cx, cz, height_cm FROM grading ORDER BY cx, cz`)
	if err != nil {
		return terrain.Grading{}, fmt.Errorf("sourcestore: правки высот: %w", err)
	}
	defer rows.Close()
	out := terrain.Grading{}
	for rows.Next() {
		var c terrain.GradeCell
		if err := rows.Scan(&c.CX, &c.CZ, &c.HeightCm); err != nil {
			return terrain.Grading{}, fmt.Errorf("sourcestore: правки высот: %w", err)
		}
		out.Cells = append(out.Cells, c)
	}
	if err := rows.Err(); err != nil {
		return terrain.Grading{}, fmt.Errorf("sourcestore: правки высот: %w", err)
	}
	return out, nil
}

// putGrading заменяет множество правок целиком. Замена, а не точечное
// обновление: правки — множество, и коммит пишет новое множество одной
// операцией; PK (cx, cz) держит атомарность клетки (двух отметок одной
// клетки форма не несёт).
func putGrading(e execer, g terrain.Grading) error {
	if _, err := e.Exec(`DELETE FROM grading`); err != nil {
		return fmt.Errorf("sourcestore: правки высот: %w", err)
	}
	for _, c := range g.Cells {
		if _, err := e.Exec(`INSERT INTO grading (cx, cz, height_cm) VALUES (?, ?, ?)`,
			c.CX, c.CZ, c.HeightCm); err != nil {
			return fmt.Errorf("sourcestore: правки высот: %w", err)
		}
	}
	return nil
}

// GetVegetation читает источники растительности. Clearings возвращаются в
// порядке вставки — порядок для компилятора значения не имеет (множество),
// но эталонное сравнение предпочитает порядок без сюрпризов.
func (s *Store) GetVegetation() (vegetation.Sources, error) {
	out := vegetation.Sources{}

	rows, err := s.db.Query(`SELECT cx, cz, i, j FROM vegetation_cuts ORDER BY cx, cz, i, j`)
	if err != nil {
		return vegetation.Sources{}, fmt.Errorf("sourcestore: вырубка: %w", err)
	}
	for rows.Next() {
		var c vegetation.Cut
		if err := rows.Scan(&c.CX, &c.CZ, &c.I, &c.J); err != nil {
			rows.Close()
			return vegetation.Sources{}, fmt.Errorf("sourcestore: вырубка: %w", err)
		}
		out.Cuts = append(out.Cuts, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return vegetation.Sources{}, fmt.Errorf("sourcestore: вырубка: %w", err)
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT cx, cz, i, j FROM vegetation_planted ORDER BY cx, cz, i, j`)
	if err != nil {
		return vegetation.Sources{}, fmt.Errorf("sourcestore: посадка: %w", err)
	}
	for rows.Next() {
		var p vegetation.Planted
		if err := rows.Scan(&p.CX, &p.CZ, &p.I, &p.J); err != nil {
			rows.Close()
			return vegetation.Sources{}, fmt.Errorf("sourcestore: посадка: %w", err)
		}
		out.Planted = append(out.Planted, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return vegetation.Sources{}, fmt.Errorf("sourcestore: посадка: %w", err)
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT cx, cz, bits FROM vegetation_cut_masks ORDER BY cx, cz`)
	if err != nil {
		return vegetation.Sources{}, fmt.Errorf("sourcestore: маски вырубки: %w", err)
	}
	for rows.Next() {
		var m vegetation.CutMask
		if err := rows.Scan(&m.CX, &m.CZ, &m.Bits); err != nil {
			rows.Close()
			return vegetation.Sources{}, fmt.Errorf("sourcestore: маски вырубки: %w", err)
		}
		out.CutMasks = append(out.CutMasks, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return vegetation.Sources{}, fmt.Errorf("sourcestore: маски вырубки: %w", err)
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT min_x, min_z, max_x, max_z FROM vegetation_clearings ORDER BY id`)
	if err != nil {
		return vegetation.Sources{}, fmt.Errorf("sourcestore: областная вырубка: %w", err)
	}
	for rows.Next() {
		var cl vegetation.Clearing
		if err := rows.Scan(&cl.MinX, &cl.MinZ, &cl.MaxX, &cl.MaxZ); err != nil {
			rows.Close()
			return vegetation.Sources{}, fmt.Errorf("sourcestore: областная вырубка: %w", err)
		}
		out.Clearings = append(out.Clearings, cl)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return vegetation.Sources{}, fmt.Errorf("sourcestore: областная вырубка: %w", err)
	}
	return out, nil
}

// PutVegetation заменяет множество источников растительности целиком.
//
// Отдельный метод (вне Tx) существует потому, что у растительности пока нет
// производителя: транспорт команд строителя — шаг 12 эпика, и до него
// множество меняет только тест или будущий коммит. Замена целиком держит
// семантику множества (идемпотентность повторной доставки), а не точечный
// аппенд.
func (s *Store) PutVegetation(v vegetation.Sources) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("sourcestore: растительность: %w", err)
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`DELETE FROM vegetation_cuts`,
		`DELETE FROM vegetation_planted`,
		`DELETE FROM vegetation_cut_masks`,
		`DELETE FROM vegetation_clearings`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("sourcestore: растительность: %w", err)
		}
	}
	for _, c := range v.Cuts {
		if _, err := tx.Exec(`INSERT INTO vegetation_cuts (cx, cz, i, j) VALUES (?, ?, ?, ?)`,
			c.CX, c.CZ, c.I, c.J); err != nil {
			return fmt.Errorf("sourcestore: растительность: %w", err)
		}
	}
	for _, p := range v.Planted {
		if _, err := tx.Exec(`INSERT INTO vegetation_planted (cx, cz, i, j) VALUES (?, ?, ?, ?)`,
			p.CX, p.CZ, p.I, p.J); err != nil {
			return fmt.Errorf("sourcestore: растительность: %w", err)
		}
	}
	for _, m := range v.CutMasks {
		if _, err := tx.Exec(`INSERT INTO vegetation_cut_masks (cx, cz, bits) VALUES (?, ?, ?)`,
			m.CX, m.CZ, m.Bits); err != nil {
			return fmt.Errorf("sourcestore: растительность: %w", err)
		}
	}
	for _, cl := range v.Clearings {
		if _, err := tx.Exec(`INSERT INTO vegetation_clearings (min_x, min_z, max_x, max_z) VALUES (?, ?, ?, ?)`,
			cl.MinX, cl.MinZ, cl.MaxX, cl.MaxZ); err != nil {
			return fmt.Errorf("sourcestore: растительность: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sourcestore: растительность: %w", err)
	}
	return nil
}

// GetCommits читает историю принятых транзакций в порядке журнала (seq).
func (s *Store) GetCommits() ([]CommitRecord, error) {
	rows, err := s.db.Query(`SELECT elements, cells FROM commits ORDER BY seq`)
	if err != nil {
		return nil, fmt.Errorf("sourcestore: история коммитов: %w", err)
	}
	defer rows.Close()
	var out []CommitRecord
	for rows.Next() {
		var elJSON, cellsJSON string
		if err := rows.Scan(&elJSON, &cellsJSON); err != nil {
			return nil, fmt.Errorf("sourcestore: история коммитов: %w", err)
		}
		var rec CommitRecord
		if err := json.Unmarshal([]byte(elJSON), &rec.Elements); err != nil {
			return nil, fmt.Errorf("sourcestore: история коммитов: %w", err)
		}
		if err := json.Unmarshal([]byte(cellsJSON), &rec.Cells); err != nil {
			return nil, fmt.Errorf("sourcestore: история коммитов: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sourcestore: история коммитов: %w", err)
	}
	return out, nil
}

// appendCommit дописывает одну запись в конец журнала. seq обязан быть ровно
// текущей длиной журнала: позиция в журнале — индекс окна конфликта edit.
func appendCommit(e execer, seq int, rec CommitRecord) error {
	// nil -> пустой JSON-массив: json.Marshal(nil) даёт null, а NULL в STRICT
	// TEXT-колонке SQLite не лежит. Пустой список и отсутствие списка — одно
	// и то же множество.
	els := append([]string(nil), rec.Elements...)
	sort.Strings(els)
	if els == nil {
		els = []string{}
	}
	cells := append([]GradeCell(nil), rec.Cells...)
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].CX != cells[j].CX {
			return cells[i].CX < cells[j].CX
		}
		return cells[i].CZ < cells[j].CZ
	})
	if cells == nil {
		cells = []GradeCell{}
	}
	elJSON, err := json.Marshal(els)
	if err != nil {
		return fmt.Errorf("sourcestore: история коммитов: %w", err)
	}
	cellsJSON, err := json.Marshal(cells)
	if err != nil {
		return fmt.Errorf("sourcestore: история коммитов: %w", err)
	}
	// string(), а не []byte: modernc/sqlite связывает []byte как BLOB, а
	// колонки commits.elements/cells — TEXT (STRICT), и BLOB в них не ляжет.
	if _, err := e.Exec(`INSERT INTO commits (seq, elements, cells) VALUES (?, ?, ?)`,
		seq, string(elJSON), string(cellsJSON)); err != nil {
		return fmt.Errorf("sourcestore: история коммитов: %w", err)
	}
	return nil
}

// Tx — атомарная пачка исходников: коммит edit кладёт закоммиченную карту,
// правки высот и запись журнала ОДНОЙ транзакцией. Частичной записи нет: сбой
// на любой строке откатывает всю пачку, и хранилище остаётся на прежнем
// поколении — читатель не видит «карта новая, правки старые».
type Tx struct {
	db *sql.Tx
	// seq — позиция следующей записи журнала: длина истории на момент Begin.
	// Дописывание в конец — окно конфликта edit читает историю по индексу, и
	// дыры в seq сделали бы его бессмысленным.
	seq int
}

// Begin открывает транзакцию записи исходников.
//
// Позиция новой записи журнала — текущая длина истории: транзакция дописывает
// в конец, а окно конфликта edit читает историю по индексу. Начало с нуля
// при непустой истории упало бы на PRIMARY KEY.
func (s *Store) Begin() (*Tx, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("sourcestore: транзакция: %w", err)
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM commits`).Scan(&n); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("sourcestore: транзакция: %w", err)
	}
	return &Tx{db: tx, seq: n}, nil
}

// PutCommittedMap кладёт закоммиченную карту в транзакцию.
func (tx *Tx) PutCommittedMap(m *mapfmt.Map) error {
	return putCommittedMap(tx.db, m)
}

// PutGrading заменяет правки высот в транзакции.
func (tx *Tx) PutGrading(g terrain.Grading) error {
	return putGrading(tx.db, g)
}

// AppendCommit дописывает запись журнала в транзакцию. Позиция записи —
// текущая длина журнала: окно конфликта edit читает историю по индексу, и
// дыры в seq сделали бы его бессмысленным.
func (tx *Tx) AppendCommit(rec CommitRecord) error {
	if err := appendCommit(tx.db, tx.seq, rec); err != nil {
		return err
	}
	tx.seq++
	return nil
}

// Commit фиксирует пачку. Откат (Rollback) после Commit — no-op.
func (tx *Tx) Commit() error {
	if err := tx.db.Commit(); err != nil {
		return fmt.Errorf("sourcestore: транзакция: %w", err)
	}
	return nil
}

// Rollback откатывает пачку целиком.
func (tx *Tx) Rollback() error { return tx.db.Rollback() }
