// Строительные транзакции: макет строителя — журнал операций.
//
// Вся правка мира идёт через макет (спека
// .internal/specs/2026-08-11-build-transactions-design.md, §1): режима прямой
// правки закоммиченного мира не существует. Макет принадлежит учётной
// записи, создаётся сессией строителя и переживает разрыв связи — сессия
// лишь ручка, а макет живёт в Service, пока живёт сервис. Срок жизни
// брошенной транзакции — открытый вопрос §12 спеки; модель его не решает:
// макет не удаляется ни временем, ни бездействием, и политика срока жизни
// остаётся за потребителем (отсутствие объявляется, а не подставляется).
//
// Макет — журнал операций, а не снимок мира: коммит переигрывает журнал на
// текущей закоммиченной базе. Отбитый коммит возвращает макет автору
// целиком — частично принятого состояния не бывает (§4), и это причина, по
// которой транзакция обязана быть журналом: переиграть можно операции,
// снимок пришлось бы мержить.
//
// Коммит — приёмка в эксплуатацию (§3). Предусловие: затронутая
// протяжённость не несёт движения. Сегодня мир не знает ни занятости, ни
// маршрутов, ни ДСП, поэтому состояние существующего элемента проверить
// нечем — коммит, трогающий его, честно отказывает; элемент, созданный при
// жизни макета, движения не нёс по построению и в зачёт не идёт. Закрытие
// ДСП — один из способов добиться «не несёт движения»; механики закрытия
// пока не существует, и это решение осознанное, а не дыра: отказ честен,
// молчаливый пропуск предусловия — нет.
//
// Конфликт — отбой на коммите, как в git: занятия участка вперёд нет (§4).
// Конфликтуют элементы пути И клетки правок высот: производный рельеф не
// конфликтует, потому что в этой модели его нет вовсе — он функция
// закоммиченных осей, и его пересчёт живёт в компиляторе (worldgen), а не в
// edit. Прямой терраморфинг (ClearAhead-26g) — первый случай, когда высоты
// редактируются напрямую: правка адресуется клеткой, и адрес клетки входит в
// предмет конфликта рядом с идентификатором элемента (спека §5).
//
// Видимость фильтруется серверно (§2): строители видят закоммиченное и все
// открытые макеты, ДСП и машинисты — только закоммиченное. Service в этом
// файле — шов, за который будущий транспорт (httpapi, шаг 12 эпика)
// дернёт модель; клиент не фильтрует, правильность не зависит от чужого
// кода.
package edit

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/sourcestore"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/uuidv7"
)

// Account — учётная запись, владелец макета. Совместного владения нет:
// макет ровно один на запись (спека §1).
type Account string

// Role — роль в системе. Решает видимость (Views); право закрытия
// протяжённости в будущем принадлежит ДСП, а не строителю самому себе
// (спека §3) — здесь оно не реализовано, см. ErrClosureRequired.
type Role int

const (
	RoleBuilder    Role = iota // строитель: макет всегда, видит чужие макеты
	RoleDispatcher             // ДСП: видит только закоммиченное
	RoleDriver                 // машинист: видит только закоммиченное
)

// Ошибки коммита. Разные причины обязаны быть различимы вызывающим по типу,
// а не по строке для человека: предусловие приёмки и конфликт — разные
// отказы (решение владельца 2026-08-11), и тест различает их errors.Is.
var (
	// ErrClosureRequired — коммит трогает существующий элемент пути, а его
	// состояние для движения неизвестно: ни занятости, ни маршрутов, ни
	// ДСП. Закрытие даёт ДСП; механики закрытия пока не существует,
	// поэтому отказ — единственный честный исход (случаи 2-3 разбора
	// владельца).
	ErrClosureRequired = errors.New("edit: протяжённость занята или её состояние неизвестно; закрытие даёт ДСП, механики закрытия пока нет")
	// ErrConflict — затронутые множества пересеклись с коммитом, лёгшим
	// после создания макета. Отбой на коммите, макет целиком возвращается
	// автору; проигравший переигрывает журнал на новой базе (спека §4).
	ErrConflict = errors.New("edit: конфликт макета с уже принятым коммитом")
)

// journalOp — одна операция журнала с зафиксированными идентификаторами.
//
// Журнал хранит не только намерение: переигровка обязана воспроизвести
// идентификаторы, выданные при первом применении, иначе предпросмотр и
// коммит разошлись бы на каждом новом прогоне — uuidv7.New даёт новый UUID
// на каждый вызов, и журнал с двумя операциями, вторая из которых ссылается
// на элемент первой, вообще не переигрался бы (идентификатор первой
// меняется, ссылка устаревает). Идентификаторы фиксируются при применении;
// переигровка отдаёт их в том же порядке, и только новая операция черпает
// из живого источника.
type journalOp struct {
	intent Intent
	ids    []string
}

// replaySource — источник тождества переигровки: сначала зафиксированные
// при первом применении идентификаторы операции, затем живой источник.
type replaySource struct {
	recorded []string
	pos      int
	live     uuidv7.Source
	drawn    []string // выданное живым источником: идентификаторы новой операции
}

func (r *replaySource) next() (string, error) {
	if r.pos < len(r.recorded) {
		id := r.recorded[r.pos]
		r.pos++
		return id, nil
	}
	id, err := r.live()
	if err != nil {
		return "", err
	}
	r.drawn = append(r.drawn, id)
	return id, nil
}

// applyJournal переигрывает журнал операций на копии базы и возвращает
// итоговую карту, затронутые множества по операциям (для предусловия и
// конфликта коммита) и каскад последней операции (для стирки). Журнал —
// единственное хранилище макета: снимков мира не существует, предпросмотр,
// коммит и выдача макетов идут одним и тем же путём.
//
// recordNew — журнал дополнен НОВОЙ операцией (последней в списке): её
// идентификаторы ещё не зафиксированы, они черпаются из живого источника и
// записываются в элемент журнала. При recordNew=false переигровка не
// касается живого источника вовсе — каждая операция обязана потребить ровно
// зафиксированные при применении идентификаторы, иначе журнал разошёлся бы с
// кодом операций.
func applyJournal(base *mapfmt.Map, journal []journalOp, live uuidv7.Source, recordNew bool) (mapfmt.Map, []touchedSet, *ErasePreview, error) {
	m := cloneMap(base)
	touched := make([]touchedSet, len(journal))
	var last *ErasePreview
	for i := range journal {
		op := &journal[i]
		touched[i] = opAffected(&m, op.intent)
		src := &replaySource{recorded: op.ids, live: live}
		res, err := applyIntent(&m, src.next, op.intent)
		if err != nil {
			return mapfmt.Map{}, nil, nil, fmt.Errorf("edit: переигровка операции %d: %w", i, err)
		}
		if recordNew && i == len(journal)-1 {
			// Новая операция: зафиксировать выданные идентификаторы, чтобы
			// следующий прогон воспроизвёл их.
			op.ids = src.drawn
			// Новая правка высот: клетка, уже правленная в ЭТОМ макете с
			// другой отметкой, — отказ. Проверка живёт здесь, а не в
			// applyGrade, потому что видит весь журнал: переигровка (recordNew
			// == false) её не повторяет — журнал не может содержать операцию,
			// которую Apply уже отказал принять.
			if op.intent.Op == OpGrade {
				if err := gradeAgainstJournal(journal[:i], op.intent.Grade); err != nil {
					return mapfmt.Map{}, nil, nil, err
				}
			}
		} else if src.pos != len(op.ids) {
			// Операция потребила иное число идентификаторов, чем было
			// зафиксировано: код операции изменился и разошёлся с журналом.
			return mapfmt.Map{}, nil, nil, fmt.Errorf("edit: операция %d: источник тождества разошёлся с журналом", i)
		}
		m = res.Map
		last = res.Cascade
	}
	return m, touched, last, nil
}

// gradeAgainstJournal проверяет новую правку высот против прежних правок
// журнала: клетка, уже правленная с другой отметкой, — два определения одного
// атома, отказ (спека §5: две площадки в общей области — одинаковая отметка
// в пределах допуска, иначе отказ; допуск не объявлен — расхождение равно
// отказу). «Последний победил» запрещён: он сделал бы результат функцией
// порядка в журнале, а инвариант §3 контракта требует порядок-независимости.
func gradeAgainstJournal(prior []journalOp, in GradeIntent) error {
	heights := make(map[gradeCellRef]int16)
	for j := range prior {
		if prior[j].intent.Op != OpGrade {
			continue
		}
		for _, c := range prior[j].intent.Grade.Cells {
			heights[gradeCellRef{cx: c.CX, cz: c.CZ}] = c.HeightCm
		}
	}
	for _, c := range in.Cells {
		k := gradeCellRef{cx: c.CX, cz: c.CZ}
		if h, ok := heights[k]; ok && h != c.HeightCm {
			return fmt.Errorf(
				"edit: правка высот: клетка (%d, %d): в одном макете две отметки — %d и %d см (спека §5); отказ",
				c.CX, c.CZ, h, c.HeightCm)
		}
	}
	return nil
}

// touchedSet — затронутое множество операции или коммита: элементы пути И
// клетки правок высот. Два ключевых пространства нарочно не сливаются:
// коридоры земляных работ не конфликтуют (производный рельеф — функция осей,
// спека §5), а клетки правок конфликтуют, потому что высоты там — данные.
type touchedSet struct {
	elements map[string]bool
	cells    map[gradeCellRef]bool
}

// gradeCellRef — адрес клетки правки высот в плане. Уровень в ключ не входит:
// terrain.Grading.Validate запретил ненулевые уровни, и два ключа одного
// плана не могут значить разное.
type gradeCellRef struct{ cx, cz int }

// opAffected — чего операция касается на карте m (состояние ДО применения):
// элементы пути, которые она изменяет или удаляет, и клетки правки высот,
// которые она переопределяет. Созданные операцией элементы сюда не входят: их
// тождество уникально, конфликтовать им не с чем, а движения они не несли.
// Множество используется и предусловием коммита, и проверкой конфликта —
// «проверяй затронутую протяжённость целиком, а не новизну созданных
// идентификаторов» (решение владельца): ветвление в действующий путь создаёт
// новые элементы, но трогает существующий, и он обязан попасть в зачёт.
func opAffected(m *mapfmt.Map, in Intent) touchedSet {
	var out touchedSet
	switch in.Op {
	case OpBranch:
		if in.Branch.Edge != "" {
			out.elements = map[string]bool{in.Branch.Edge: true}
		}
	case OpErase:
		out.elements = eraseClosure(m, in.Erase.Target)
	case OpGrade:
		out.cells = gradingCells(in.Grade)
	}
	return out
}

// pathElements — элементы пути карты: рёбра и стрелки. Узлы и сооружения —
// не протяжённость: по ним не закрывают перегон и по ним не конфликтуют
// (спека §3, §5).
func pathElements(m *mapfmt.Map) map[string]bool {
	out := make(map[string]bool, len(m.Topology.Edges)+len(m.Topology.Turnouts))
	for _, e := range m.Topology.Edges {
		out[e.ID] = true
	}
	for _, t := range m.Topology.Turnouts {
		out[t.ID] = true
	}
	return out
}

// createdPath — элементы пути итоговой карты, которых не было в прежней
// закоммиченной: их создал этот коммит, и они входят в его затронутое
// множество — другой макет мог врезаться в них, и конфликт обязан это
// увидеть.
func createdPath(m, pre *mapfmt.Map) map[string]bool {
	before := pathElements(pre)
	out := map[string]bool{}
	for id := range pathElements(m) {
		if !before[id] {
			out[id] = true
		}
	}
	return out
}

// unionTouched — объединение затронутых множеств: и элементы, и клетки.
// Слитые множества идут в запись коммита (commitRecord) и в проверку
// конфликта; предусловие приёмки читает только элементы — правка высот
// движения не несёт, и закрытия ей не нужно.
func unionTouched(sets ...touchedSet) touchedSet {
	out := touchedSet{elements: map[string]bool{}, cells: map[gradeCellRef]bool{}}
	for _, s := range sets {
		for id := range s.elements {
			out.elements[id] = true
		}
		for k := range s.cells {
			out.cells[k] = true
		}
	}
	return out
}

// Transaction — макет строителя: журнал операций, принадлежащий учётной
// записи. Живёт в сервисе, а не в сессии: разрыв связи макет не
// уничтожает, при переподключении он подбирается обратно (спека §1).
//
// baseElements — элементы пути закоммиченного мира на момент создания
// макета; createdSeq — число коммитов на тот же момент. Оба фиксируют
// «существовал ли элемент при жизни макета»: этим решается предусловие
// приёмки (новый элемент движения не нёс) и окно конфликта (коммиты после
// создания).
type Transaction struct {
	account      Account
	ops          []journalOp
	baseElements map[string]bool
	createdSeq   int
}

// commitRecord — затронутое множество принятого коммита: элементы пути,
// которые он создал или изменил, и клетки правок высот. История записей —
// база проверки конфликта на коммите.
type commitRecord struct {
	touched touchedSet
}

// Service — серверная сторона строительных транзакций: закоммиченный мир,
// открытые макеты и история коммитов. Это шов, за который будущий транспорт
// (httpapi, шаг 12 эпика) дернёт модель.
//
// grading — правки высот закоммиченного мира: ИСХОДНИК рядом с картой, а не
// проекция. Правки НЕ ложатся в базу чанков (worldstore) — они живут с
// коммиченным миром, и пересев (снос базы ключом -reseed) их не касается:
// проекцию сносить можно всегда, исходник — нет (решение W6-B по биде 26g,
// разбор — в terrain/grading.go).
type Service struct {
	// store — хранилище исходников партии (sqym.18). nil — сервис без
	// хранилища (память, прежнее поведение тестов); с хранилищем коммит
	// пишет закоммиченное на диск, и оно переживает перезапуск и -reseed.
	store     *sourcestore.Store
	committed mapfmt.Map
	grading   map[gradeCellRef]int16
	ids       uuidv7.Source
	txs       map[Account]*Transaction
	commits   []commitRecord
}

// NewService открывает мир над закоммиченной картой. Начальная карта
// обязана проходить валидацию: операции применяются к заведомо допустимой
// базе. ids — источник тождества новых элементов (UUIDv7); тест
// подставляет детерминированный, и переигровка журнала воспроизводит
// идентификаторы байт в байт.
func NewService(committed *mapfmt.Map, ids uuidv7.Source) (*Service, error) {
	return newServiceBase(committed, ids, nil)
}

// newServiceBase — общий конструктор: сервис над картой и (опционально)
// хранилищем исходников. Хранилище даёт коммиту диск; без него поведение
// прежнее — память (NewService).
func newServiceBase(committed *mapfmt.Map, ids uuidv7.Source, store *sourcestore.Store) (*Service, error) {
	if ids == nil {
		return nil, fmt.Errorf("edit: не задан источник тождества новых элементов")
	}
	if err := mapfmt.Validate(committed); err != nil {
		return nil, fmt.Errorf("edit: исходная карта не проходит валидацию: %w", err)
	}
	return &Service{
		store:     store,
		committed: cloneMap(committed),
		grading:   map[gradeCellRef]int16{},
		ids:       ids,
		txs:       map[Account]*Transaction{},
	}, nil
}

// OpenSession открывает сессию строителя. Макет есть у строителя всегда
// (спека §1): действия «открыть транзакцию» нет — при первом входе
// учётной записи макет создаётся, при повторном подбирается обратно.
// Сессия — ручка к макету учётной записи; несколько сессий одной записи
// работают с одним макетом.
func (s *Service) OpenSession(account Account) (*Session, error) {
	if account == "" {
		return nil, fmt.Errorf("edit: не указана учётная запись")
	}
	if _, ok := s.txs[account]; !ok {
		s.txs[account] = &Transaction{
			account:      account,
			baseElements: pathElements(&s.committed),
			createdSeq:   len(s.commits),
		}
	}
	return &Session{svc: s, account: account}, nil
}

// Views — срез мира для роли: закоммиченное плюс (строителю) открытые
// макеты. Клиент не фильтрует — решение принимает серверный код.
//
// Grading — правки высот закоммиченного мира: исходник, который компилятор
// (worldgen) потребляет рядом с картой. Не-строитель видит их только в виде
// скомпилированной земли; сам исходник — часть среза строителя, как и
// закоммиченная карта.
type Views struct {
	Committed mapfmt.Map
	Grading   terrain.Grading
	Mockups   []MockupView
}

// MockupView — открытый макет: владелец, журнал операций и его текущее
// состояние (журнал, переигранный на закоммиченной базе).
type MockupView struct {
	Account Account
	Ops     []Intent
	Map     mapfmt.Map
}

// Views отдаёт срез мира для роли. Фильтрация серверная (решение владельца
// 2026-08-11): строители видят закоммиченное и все открытые макеты, ДСП и
// машинисты — только закоммиченное. Незакоммиченная геометрия не уходит
// не-строителю вовсе, даже с пометкой «не рисовать».
func (s *Service) Views(role Role) (Views, error) {
	switch role {
	case RoleBuilder, RoleDispatcher, RoleDriver:
	default:
		return Views{}, fmt.Errorf("edit: неизвестная роль %d", role)
	}
	v := Views{Committed: cloneMap(&s.committed), Grading: s.gradingView()}
	if role != RoleBuilder {
		return v, nil
	}
	accounts := make([]string, 0, len(s.txs))
	for acc := range s.txs {
		accounts = append(accounts, string(acc))
	}
	sort.Strings(accounts)
	for _, acc := range accounts {
		tx := s.txs[Account(acc)]
		m, _, _, err := applyJournal(&s.committed, tx.ops, s.ids, false)
		if err != nil {
			return Views{}, fmt.Errorf("edit: макет %s не переигрывается: %w", acc, err)
		}
		v.Mockups = append(v.Mockups, MockupView{
			Account: tx.account,
			Ops:     journalIntents(tx.ops),
			Map:     m,
		})
	}
	return v, nil
}

// journalIntents — копия намерений журнала для отдачи наружу: мутация
// выданного списка не должна трогать хранимый журнал.
func journalIntents(ops []journalOp) []Intent {
	out := make([]Intent, len(ops))
	for i := range ops {
		out[i] = ops[i].intent
	}
	return out
}

// Session — сессия строителя: ручка к макету учётной записи. Разрыв связи
// макет не уничтожает — сессию можно отбросить, и новая сессия той же
// записи подберёт макет целиком.
type Session struct {
	svc     *Service
	account Account
}

// gradingView — правки высот закоммиченного мира как список клеток для
// компилятора (worldgen потребляет terrain.Grading). Порядок детерминирован:
// компилятор чист, и два разных порядка одного множества дали бы одинаковые
// чанки, но эталонное сравнение и журнал предпочитают порядок без сюрпризов.
func (s *Service) gradingView() terrain.Grading {
	return gradingToStore(s.grading)
}

func (s *Session) tx() *Transaction {
	return s.svc.txs[s.account]
}

// Apply добавляет операцию в журнал макета: переигрывает журнал с новой
// операцией на текущей закоммиченной базе и только при успехе фиксирует
// её. Неудача не оставляет следа — журнал и мир не меняются вовсе.
// Пересборка проекций (worldgen) здесь не зовётся: предпросмотр макета —
// без пересборки, авторитетный пересчёт — по коммиту, и делает его
// компилятор-потребитель.
func (s *Session) Apply(i Intent) (Result, error) {
	tx := s.tx()
	cand := make([]journalOp, 0, len(tx.ops)+1)
	cand = append(cand, tx.ops...)
	cand = append(cand, journalOp{intent: i})
	m, _, last, err := applyJournal(&s.svc.committed, cand, s.svc.ids, true)
	if err != nil {
		return Result{}, err
	}
	tx.ops = cand
	return Result{Map: m, Cascade: last}, nil
}

// Preview — тот же расчёт, что Apply, без роста журнала: перетаскивание
// мышью ничего не меняет, а предпросмотр и факт не могут разойтись.
func (s *Session) Preview(i Intent) (Result, error) {
	tx := s.tx()
	cand := make([]journalOp, 0, len(tx.ops)+1)
	cand = append(cand, tx.ops...)
	cand = append(cand, journalOp{intent: i})
	m, _, last, err := applyJournal(&s.svc.committed, cand, s.svc.ids, true)
	if err != nil {
		return Result{}, err
	}
	return Result{Map: m, Cascade: last}, nil
}

// Mockup — текущее состояние макета: журнал, переигранный на текущей
// закоммиченной базе. База меняется чужими коммитами — макет следует за
// ней: это журнал, а не снимок.
func (s *Session) Mockup() (mapfmt.Map, error) {
	m, _, _, err := applyJournal(&s.svc.committed, s.tx().ops, s.svc.ids, false)
	return m, err
}

// Journal — операции журнала. Макет хранит операции, а не снимок мира:
// отбитый коммит переигрывается на новой базе теми же операциями, и
// результат либо получается, либо честно отказывает.
func (s *Session) Journal() []Intent {
	return journalIntents(s.tx().ops)
}

// Commit — приёмка макета в эксплуатацию. Переигрывает журнал на текущей
// закоммиченной базе и при успехе трёх проверок принимает результат
// целиком:
//
//  1. переигровка — журнал обязан примениться к текущей базе; не прошла —
//     откат полный, макет целиком возвращается автору (спека §4);
//  2. предусловие — затронутая протяжённость не несла движения;
//  3. конфликт — затронутые множества не пересеклись с коммитами, лёгшими
//     после создания макета.
//
// Частично принятого состояния не бывает ни на миг. Авторитетный пересчёт
// проекций по принятой карте — дело компилятора (worldgen), а не edit:
// коммит отдаёт карту, потребитель пересобирает.
func (s *Session) Commit() error {
	svc := s.svc
	tx := s.tx()
	if len(tx.ops) == 0 {
		return fmt.Errorf("edit: коммит пустой транзакции: коммитить нечего (спека §1)")
	}

	m, touched, _, err := applyJournal(&svc.committed, tx.ops, svc.ids, false)
	if err != nil {
		return err
	}

	// Предусловие приёмки: затронутая протяжённость не несла движения.
	// Состояние элемента, существовавшего при создании макета, проверить
	// нечем — ни занятости, ни маршрутов, ни ДСП; отказ честен (случаи 2-3
	// разбора владельца). Элемент, созданный при жизни макета, движения не
	// нёс по построению (случай 1) — в зачёт не идёт. Клетки правок высот в
	// предусловие НЕ входят: правка земли движения не несёт, и закрытия ей
	// не нужно (спека §3 — предусловие висит на пути).
	var affected map[string]bool
	for _, t := range touched {
		for id := range t.elements {
			if tx.baseElements[id] {
				if affected == nil {
					affected = map[string]bool{}
				}
				affected[id] = true
			}
		}
	}
	if len(affected) > 0 {
		return fmt.Errorf("%w: элементы %s", ErrClosureRequired, strings.Join(sortedSet(affected), ", "))
	}

	// Конфликт: пересечение с коммитами, лёгшими после создания макета.
	// Проверка на коммите, занятия участка вперёд нет (спека §4).
	// Конфликтуют и элементы, и клетки: два макета, правящие ОДНУ клетку,
	// — два определения одного атома данных (спека §5), коридоры же
	// производного рельефа не конфликтуют — их в затронутом множестве нет
	// вовсе (TestEarthworksCorridorsDoNotConflict).
	own := unionTouched(touched...)
	conflictEls := map[string]bool{}
	conflictCells := map[gradeCellRef]bool{}
	for _, rec := range svc.commits[tx.createdSeq:] {
		for id := range own.elements {
			if rec.touched.elements[id] {
				conflictEls[id] = true
			}
		}
		for k := range own.cells {
			if rec.touched.cells[k] {
				conflictCells[k] = true
			}
		}
	}
	var parts []string
	if len(conflictEls) > 0 {
		parts = append(parts, "элементы "+strings.Join(sortedSet(conflictEls), ", "))
	}
	if len(conflictCells) > 0 {
		parts = append(parts, "клетки "+strings.Join(sortedCells(conflictCells), ", "))
	}
	if len(parts) > 0 {
		return fmt.Errorf("%w: %s", ErrConflict, strings.Join(parts, "; "))
	}

	// Приёмка: закоммиченный мир заменяется результатом переигровки
	// целиком, мировая ревизия растёт ровно на один шаг — вернуть её назад
	// нечем, отмены нет. Макет переходит в новое состояние: журнал пуст, а
	// созданные коммитом элементы теперь существуют — следующий заход на
	// них уже требует закрытия.
	//
	// Правки высот сворачиваются в закоммиченный мир ТОТ ЖЕ транзакцией
	// приёмки: клетка, правленная коммитом, переопределяет прежнюю отметку
	// (журнал сервера линеен, «последний победил» здесь — не гонка доставки,
	// а сам журнал, спека §5); конфликт с чужим макетом уже отбит выше.
	m.MapRevision = svc.committed.MapRevision + 1
	record := unionTouched(touched...)
	for id := range createdPath(&m, &svc.committed) {
		record.elements[id] = true
	}
	grading := make(map[gradeCellRef]int16, len(svc.grading)+len(record.cells))
	for k, v := range svc.grading {
		grading[k] = v
	}
	for _, op := range tx.ops {
		if op.intent.Op != OpGrade {
			continue
		}
		for _, c := range op.intent.Grade.Cells {
			grading[gradeCellRef{cx: c.CX, cz: c.CZ}] = c.HeightCm
		}
	}

	// ДИСК ПРЕЖДЕ ПАМЯТИ: хранилище фиксируется ДО смены состояния сервиса.
	// Отказ записи — отказ коммита целиком: макет остаётся у автора, мир и
	// память не тронуты. Обратный порядок оставил бы память впереди диска на
	// случай сбоя процесса между ними — ровно та тихая потеря, ради которой
	// хранилище существует (sqym.18).
	if svc.store != nil {
		if err := persistCommit(svc.store, &m, grading, commitRecord{touched: record}); err != nil {
			return err
		}
	}
	svc.commits = append(svc.commits, commitRecord{touched: record})
	svc.committed = m
	svc.grading = grading
	tx.ops = nil
	tx.baseElements = pathElements(&svc.committed)
	tx.createdSeq = len(svc.commits)
	return nil
}

func sortedCells(cells map[gradeCellRef]bool) []string {
	keys := make([]gradeCellRef, 0, len(cells))
	for k := range cells {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].cx != keys[j].cx {
			return keys[i].cx < keys[j].cx
		}
		return keys[i].cz < keys[j].cz
	})
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = fmt.Sprintf("(%d, %d)", k.cx, k.cz)
	}
	return out
}
