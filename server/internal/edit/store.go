// Персистентность исходников партии: закоммиченное — на диск, в sourcestore.
//
// До sqym.18 закоммиченный мир жил только в памяти Service (tx.go): правки
// высот и принятые транзакции умирали вместе с процессом, а -reseed, сносящий
// базу проекций, делал это незаметным — мир пересобирался без правок игрока,
// и выглядело это как «правка не сработала». Хранилище исходников (sourcestore)
// — отдельный файл SQLite, которого -reseed не касается; этот файл кладёт и
// поднимает закоммиченное.
//
// Форма записи — состояние, а не журнал команд: закоммиченная карта целиком,
// множество правок высот, история коммитов (затронутые множества). Переигровка
// команд не нужна — «отмены нет и не будет» (решение владельца, edit.go), и
// авторитетный факт — принятое состояние, а не последовательность, которая к
// нему привела.
package edit

import (
	"fmt"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/sourcestore"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/uuidv7"
)

// NewServiceStored — сервис правок над хранилищем исходников: закоммиченное
// поднимается из sourcestore при старте, а не с нуля. seed — карта-затравка
// ПЕРВОЙ сессии: пока хранилище пусто, закоммиченный мир — это она; со второй
// сессии хранилище авторитетно, и карта-файл остаётся только рецептом природы
// (сверка — в worldgen, sameRule/sameDomain).
//
// Открытые макеты при этом НЕ поднимаются: макет — рабочая доска строителя,
// черновик, и черновики умирают вместе с процессом, как несохранённые буферы
// редактора. Закоммиченное — факт мира и лежит на диске; черновик — частное
// состояние сессии, и переносить его в хранилище значило бы угадывать форму
// транспорта (шаг 12 эпика) раньше, чем он появился.
func NewServiceStored(store *sourcestore.Store, seed *mapfmt.Map, ids uuidv7.Source) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("edit: хранилище исходников не задано")
	}
	committed := seed
	if stored, ok, err := store.GetCommittedMap(); err != nil {
		return nil, err
	} else if ok {
		committed = stored
	}
	svc, err := newServiceBase(committed, ids, store)
	if err != nil {
		return nil, err
	}
	g, err := store.GetGrading()
	if err != nil {
		return nil, err
	}
	svc.grading, err = gradingFromStore(g)
	if err != nil {
		return nil, err
	}
	recs, err := store.GetCommits()
	if err != nil {
		return nil, err
	}
	svc.commits = make([]commitRecord, 0, len(recs))
	for _, r := range recs {
		svc.commits = append(svc.commits, commitRecordFromStore(r))
	}
	return svc, nil
}

// Committed — закоммиченный мир: авторитетный исходник пути, который
// компиляторы (worldgen) потребляют рядом с картой. Копия: мутация выданного
// не трогает сервис.
func (s *Service) Committed() mapfmt.Map {
	return cloneMap(&s.committed)
}

// Grading — правки высот закоммиченного мира для компилятора (worldgen).
func (s *Service) Grading() terrain.Grading {
	return s.gradingView()
}

// JournalSeq — позиция журнала коммитов: сколько транзакций принято. Голова
// проекций называет эту позицию при публикации (worldstore.Publish), и
// компилятор мира строит мир «до неё» (worldgen.Compiler).
func (s *Service) JournalSeq() int64 {
	return int64(len(s.commits))
}

// persistCommit кладёт принятый коммит в хранилище исходников ОДНОЙ
// транзакцией: закоммиченная карта, правки высот, запись журнала. Частичной
// записи нет — сбой на любой строке откатывает всю пачку, и хранилище
// остаётся на прежнем поколении: читатель не видит «карта новая, правки
// старые».
func persistCommit(st *sourcestore.Store, m *mapfmt.Map, grading map[gradeCellRef]int16, rec commitRecord) error {
	tx, err := st.Begin()
	if err != nil {
		return fmt.Errorf("edit: коммит не лёг в хранилище исходников: %w", err)
	}
	if err := tx.PutCommittedMap(m); err != nil {
		tx.Rollback()
		return fmt.Errorf("edit: коммит не лёг в хранилище исходников: %w", err)
	}
	if err := tx.PutGrading(gradingToStore(grading)); err != nil {
		tx.Rollback()
		return fmt.Errorf("edit: коммит не лёг в хранилище исходников: %w", err)
	}
	if err := tx.AppendCommit(commitRecordToStore(rec)); err != nil {
		tx.Rollback()
		return fmt.Errorf("edit: коммит не лёг в хранилище исходников: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("edit: коммит не лёг в хранилище исходников: %w", err)
	}
	return nil
}

// gradingToStore — множество правок высот как список клеток для хранилища.
// Порядок детерминирован: компилятор чист, но эталонное сравнение и журнал
// предпочитают порядок без сюрпризов.
func gradingToStore(g map[gradeCellRef]int16) terrain.Grading {
	if len(g) == 0 {
		return terrain.Grading{}
	}
	out := make([]terrain.GradeCell, 0, len(g))
	for k, h := range g {
		out = append(out, terrain.GradeCell{Level: 0, CX: k.cx, CZ: k.cz, HeightCm: h})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CX != out[j].CX {
			return out[i].CX < out[j].CX
		}
		return out[i].CZ < out[j].CZ
	})
	return terrain.Grading{Cells: out}
}

// gradingFromStore — правки высот из хранилища как индекс сервиса. Валидация
// на входе, а не при первом применении: хранилище с клеткой не уровня 0 —
// испорченное хранилище, и отказ старта честнее молчаливой починки. Пустое
// множество законно — мир без правок.
func gradingFromStore(g terrain.Grading) (map[gradeCellRef]int16, error) {
	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("edit: правки высот из хранилища: %w", err)
	}
	out := make(map[gradeCellRef]int16, len(g.Cells))
	for _, c := range g.Cells {
		out[gradeCellRef{cx: c.CX, cz: c.CZ}] = c.HeightCm
	}
	return out, nil
}

// commitRecordToStore — затронутое множество коммита в форме хранилища.
func commitRecordToStore(rec commitRecord) sourcestore.CommitRecord {
	out := sourcestore.CommitRecord{}
	for id := range rec.touched.elements {
		out.Elements = append(out.Elements, id)
	}
	for k := range rec.touched.cells {
		out.Cells = append(out.Cells, sourcestore.GradeCell{CX: k.cx, CZ: k.cz})
	}
	return out
}

// commitRecordFromStore — запись журнала хранилища как затронутое множество.
func commitRecordFromStore(rec sourcestore.CommitRecord) commitRecord {
	out := commitRecord{touched: touchedSet{
		elements: map[string]bool{},
		cells:    map[gradeCellRef]bool{},
	}}
	for _, id := range rec.Elements {
		out.touched.elements[id] = true
	}
	for _, c := range rec.Cells {
		out.touched.cells[gradeCellRef{cx: c.CX, cz: c.CZ}] = true
	}
	return out
}
