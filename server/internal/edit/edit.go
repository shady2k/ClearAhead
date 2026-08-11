// Package edit — применение правок к карте и управление ревизиями.
//
// Редактор карт: игрок тянет мышью, решатель на сервере считает законную
// трассу, этот пакет применяет результат к карте. Применение атомарно и
// возвращает новую ревизию карты целиком; каждая применённая правка проходит
// mapfmt.Validate — вердикт валидатора, а не наша уверенность. Неудачная
// правка не оставляет полуприменённого состояния: карта остаётся ровно такой,
// какой была.
//
// Отмена — возврат на предыдущую ревизию из стека ревизий в памяти; стек
// команд не изобретается. Ревизия рождается только на применении правки;
// предпросмотр (Preview) — чистый расчёт без побочных эффектов. Пакет не
// пишет на диск: сохранение и загрузка — чужая задача (mapstore), которая
// вызывает нас, а не наоборот.
//
// Входной тип узкий и свой: тип результата решателя ещё не готов, поэтому
// Intent несёт намерение + цепочку примитивов + куда прикладывать. Сшивание с
// решателем — отдельная задача.
package edit

import (
	"encoding/json"
	"fmt"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// Op — вид правки.
type Op int

const (
	OpExtend Op = iota // продлить путь от порта
	OpBranch           // ответвиться от существующего элемента (появляется стрелка)
	OpCap              // замкнуть конец тупиковым упором
	OpPlace            // положить платформу на участок
	OpErase            // стереть
)

// Intent — намерение игрока, готовое к применению: что сделать, цепочка
// примитивов и куда прикладывать. Поля заполняются по Op; остальные
// игнорируются.
type Intent struct {
	Op Op

	Extend ExtendIntent // OpExtend
	Branch BranchIntent // OpBranch
	Cap    CapIntent    // OpCap
	Place  PlaceIntent  // OpPlace
	Erase  EraseIntent  // OpErase
}

// ExtendIntent — продлить путь от порта.
//
// Порт обязан быть листом (ровно одно ребро) и концом приходящего пути (ребро
// заканчивается в порту): направление продолжения — по приходящему пути.
// Новое ребро начинается в порту, новый конец получает назначение.
type ExtendIntent struct {
	// Port — порт, от которого продолжаем.
	Port string
	// Chain — цепочка примитивов нового ребра в системе позы порта.
	Chain geom.Chain
	// EndPurpose — назначение нового конца: "" — buffer_stop.
	EndPurpose string
}

// BranchIntent — ответвиться от существующего ребра стрелкой.
//
// Ребро Edge режется в точке AtU (строго внутри): подходная часть остаётся
// ребром Edge и приходит в общий порт новой стрелки, продолжение — новое ребро
// от прямого порта, ветвь — новое ребро от отклонённого порта.
type BranchIntent struct {
	Edge string  // существующее ребро
	AtU  float64 // точка ответвления, метры в координате u
	Hand string  // "left" | "right"

	// Straight и Diverging — геометрия проходов стрелки: прямого и
	// отклонённого. Branch — геометрия ветви после отклонённого прохода.
	// Проходы задаёт намерение: законную геометрию считает решатель.
	Straight  geom.Chain
	Diverging geom.Chain
	Branch    geom.Chain
	// EndPurpose — назначение конца ветви: "" — buffer_stop.
	EndPurpose string
}

// CapIntent — замкнуть конец тупиковым упором: порт получает purpose
// buffer_stop.
type CapIntent struct {
	Port string
}

// PlaceIntent — положить платформу на участок элемента.
type PlaceIntent struct {
	Element string  // элемент (ребро или проход стрелки)
	From    float64 // начало интервала, метры u
	To      float64 // конец интервала, метры u
	Side    string  // "left" | "right"; "" — "right"
	Offset  float64 // от оси пути до ближней кромки, метры
	Width   float64 // поперёк пути, метры
}

// EraseMode — режим стирки.
type EraseMode int

const (
	// EraseSelection — удалить только выбранное, если граф остаётся
	// допустимым: каскад не может уносить элементы сверх выбранного.
	EraseSelection EraseMode = iota
	// EraseCascade — удалить конструкцию целиком с показанными зависимостями:
	// каскад добирает всё, что осиротеет вместе с целью.
	EraseCascade
)

// EraseIntent — стереть элемент (ребро или стрелку).
type EraseIntent struct {
	Target string // ID ребра или стрелки
	Mode   EraseMode
}

// Result — итог применения правки.
type Result struct {
	// Map — новая ревизия карты целиком.
	Map mapfmt.Map
	// Revision — map_revision новой ревизии.
	Revision int
	// Cascade — предпросмотр каскада стирки; nil для остальных правок.
	Cascade *ErasePreview
}

// ErasePreview — каскад стирки: что уходит, чьи спаны порвались, какие концы
// закрываются упором.
//
// Предпросмотр обязан совпадать с фактическим результатом — игрок принимает
// решение по нему. Списки отсортированы по ID.
type ErasePreview struct {
	RemovedElements  []string // рёбра и стрелки, которые уходят (включая цель)
	RemovedTrackside []string // путевые объекты, чьи спаны порвались
	CappedPorts      []string // концы, ставшие висящими и закрытые упором
}

// Store — стек ревизий одной карты в памяти.
type Store struct {
	stack  []mapfmt.Map
	maxRev int // максимальная ревизия, когда-либо бывшая в стеке
}

// NewStore открывает редактор над картой. Начальная карта обязана проходить
// валидацию: правки применяются к заведомо допустимой базе.
func NewStore(m *mapfmt.Map) (*Store, error) {
	if err := mapfmt.Validate(m); err != nil {
		return nil, fmt.Errorf("edit: исходная карта не проходит валидацию: %w", err)
	}
	s := &Store{stack: []mapfmt.Map{cloneMap(m)}}
	s.maxRev = s.stack[0].MapRevision
	return s, nil
}

// Current возвращает копию текущей ревизии.
func (s *Store) Current() mapfmt.Map {
	return cloneMap(&s.stack[len(s.stack)-1])
}

// Revision возвращает номер текущей ревизии.
func (s *Store) Revision() int {
	return s.stack[len(s.stack)-1].MapRevision
}

// Undo откатывает на предыдущую ревизию. Возвращает false, если откатывать
// некуда (в стеке одна ревизия).
func (s *Store) Undo() (mapfmt.Map, bool) {
	if len(s.stack) <= 1 {
		return mapfmt.Map{}, false
	}
	s.stack = s.stack[:len(s.stack)-1]
	return cloneMap(&s.stack[len(s.stack)-1]), true
}

// Apply применяет правку: строит результат, валидирует и только потом
// фиксирует новую ревизию. Неудача не оставляет полуприменённого состояния —
// стек не меняется вовсе.
func (s *Store) Apply(i Intent) (Result, error) {
	res, err := s.Preview(i)
	if err != nil {
		return Result{}, err
	}
	s.maxRev = res.Revision
	s.stack = append(s.stack, cloneMap(&res.Map))
	return res, nil
}

// Preview считает результат правки без побочных эффектов: стек ревизий не
// меняется, ревизия не рождается. Перетаскивание — чистый расчёт.
func (s *Store) Preview(i Intent) (Result, error) {
	return applyIntent(&s.stack[len(s.stack)-1], s.maxRev, i)
}

// cloneMap — глубокая копия карты. Ревизии в стеке обязаны быть независимы:
// мутация одной не должна трогать другую. JSON-круговорот точен для float64 и
// покрывает все поля формата; карта всегда сериализуема.
func cloneMap(m *mapfmt.Map) mapfmt.Map {
	raw, err := json.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("edit: копирование карты: %v", err))
	}
	var c mapfmt.Map
	if err := json.Unmarshal(raw, &c); err != nil {
		panic(fmt.Sprintf("edit: копирование карты: %v", err))
	}
	return c
}
