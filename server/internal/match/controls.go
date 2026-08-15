package match

import (
	"fmt"

	"github.com/shady2k/ClearAhead/server/internal/brake"

	"github.com/shady2k/ClearAhead/server/internal/content"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/protocol"
)

// Органы управления кабины: состояние актуаторов.
//
// # Четвёртый класс фактов встал на своё место
//
// Классов четыре, и они разведены нарочно (разбор — ClearAhead-0na):
//
//	ПАСПОРТ            какая машина бывает: масса, тяга, тормоз, СКОЛЬКО
//	                   ступеней у контроллера. Живёт в наборе контента.
//	АКТУАТОРЫ          что сейчас выставлено: ступень тяги, ступень тормоза,
//	                   реверсор. ЗДЕСЬ.
//	СОСТОЯНИЕ ФИЗИКИ   что из этого вышло: положение, скорость. Приедет с В3.
//	ПРОИЗВОДНОЕ        что показывает прибор. Считается из состояния физики, а
//	                   не из положения рукоятки.
//
// Смешать актуаторы с производным — обычная и дорогая ошибка: прибор,
// показывающий положение рукоятки вместо действительной силы, врёт ровно в тот
// момент, когда машинист смотрит на него, чтобы понять, почему машина не едет.
//
// # Актуаторы — ЕДИНСТВЕННЫЙ вход в физику
//
// Человек пишет сюда рукоятками, будущий автопилот (В6) — целью ведения через
// тот же выход. Физика не знает, кто писал, и второго входа не получает: иначе
// «хороший машинист», с которым человек соревнуется, ехал бы по другим
// правилам, чем человек, и сравнение потеряло бы смысл (sim-core-design §11).
//
// # Ноль здесь ЗНАЧИТ, а не «не заполнено»
//
// Правило проекта «ноль в контракте неотличим от не заполнено» действует там,
// где поле заведено раньше того, что его считает. Здесь наоборот: нулевая
// ступень тяги и реверсор в нуле — это ПОЛОЖЕНИЕ СТОЯЩЕЙ МАШИНЫ, законное
// состояние мира и то самое, в котором машина стоит на путях сейчас. Поэтому
// состояние заводится сразу у всех единиц с органами, а не появляется от
// первой команды.

// Reverser — положение реверсивной рукоятки.
//
// Три значения, и это ОБЪЯВЛЕННОЕ УПРОЩЕНИЕ: у настоящей рукоятки ВЛ80С их
// пять — ноль, «вперёд», три ступени ослабления возбуждения и «назад».
// Ослабление возбуждения физика не выражает вовсе, и заводить положения,
// которые ничего не меняют, значило бы поставить на пульт рукоятку-обманку.
type Reverser string

const (
	// ReverserNeutral — реверсор в нуле: тяга не собирается.
	ReverserNeutral Reverser = "neutral"
	ReverserForward Reverser = "forward"
	ReverserReverse Reverser = "reverse"
)

// Known — известное ли это положение.
func (r Reverser) Known() bool {
	return r == ReverserNeutral || r == ReverserForward || r == ReverserReverse
}

// Controls — положение органов управления ОДНОЙ машины.
type Controls struct {
	// Traction — ступень контроллера тяги: 0..traction_notches паспорта.
	Traction int `json:"traction"`
	// Brake — ступень служебного торможения: 0..brake_notches паспорта.
	//
	// ПРИМЕНЯЕТСЯ ТОЛЬКО У МАШИНЫ БЕЗ ПНЕВМАТИКИ. У машины с магистралью глубину
	// торможения задаёт разрядка, и ступеней нет — есть положение крана. Поле
	// оставлено, а не снесено, потому что тормозная система у машин РАЗНАЯ, и
	// «одно число вместо давления» — законная система, а не недоделка.
	Brake int `json:"brake"`
	// Reverser — положение реверсора.
	Reverser Reverser `json:"reverser"`
	// Handle — положение ручки крана машиниста. Пусто у машины без пневматики.
	Handle brake.Handle `json:"handle,omitempty"`
	// Independent — задание крана вспомогательного тормоза локомотива.
	//
	// ЗДЕСЬ, А НЕ В СОСТОЯНИИ ПНЕВМАТИКИ, потому что это КОМАНДА: машинист
	// поставил ручку, кран держит. Давления — следствие, и они живут в
	// Match.Air; смешать их значило бы позволить клиенту «выставить» давление в
	// магистрали.
	Independent brake.Pressure `json:"independent,omitempty"`
}

// Stopped — положение органов у стоящей машины.
//
// Функцией, а не переменной: значение возвращается копией, и общую переменную
// нельзя случайно править из вызывающего.
func Stopped() Controls {
	return Controls{Traction: 0, Brake: 0, Reverser: ReverserNeutral}
}

// StoppedWithAir — то же у машины С ПНЕВМАТИКОЙ: ручка крана в ПОЕЗДНОМ
// положении.
//
// Поездное, а не перекрыша и не торможение, и это решение о начале партии:
// машина поставлена на путь с заряженной магистралью (brake.Charged), а
// поездное — единственное положение, которое эту зарядку поддерживает. Любое
// другое означало бы, что партия начинается с ручки, которую никто не ставил.
func StoppedWithAir() Controls {
	c := Stopped()
	c.Handle = brake.HandleRun
	return c
}

// SetControls ставит органы управления единицы.
//
// # Ставится ВСЁ РАЗОМ, а не по одному органу
//
// Частичная правка потребовала бы отличать «не трогать» от «поставить ноль», а
// это ровно та двусмысленность, которую проект запрещает: поле, которого нет,
// и поле со значением ноль стали бы разными сообщениями с одинаковой записью.
// Полное положение кабины ещё и идемпотентно само по себе — повтор ставит то
// же самое.
//
// # Отказ возвращается МАШИННОЙ ПРИЧИНОЙ, а не текстом
//
// protocol.Refusal живёт в пакете провода, и домен, возвращающий его, выглядит
// перевёрнутой зависимостью. Она выбрана сознательно: reason читается МАШИНОЙ и
// потому является частью контракта наравне с именами полей. Обратная развязка —
// свои типы ошибок в домене плюс перевод их в причины транспортом — означала бы
// таблицу соответствия, которую надо помнить пополнять, и первый же забытый
// перевод отдал бы клиенту «внутреннюю ошибку» вместо «ступени такой нет».
//
// # Проверяется ПАСПОРТОМ, а не общим правилом
//
// Пределы ступеней — свойство машины, а не мира: у ВЛ80 их 33, у следующей
// машины будет своё число. Проверка, написанная константой, разошлась бы с
// паспортом на первой же второй машине.
func (m *Match) SetControls(unitID string, c Controls, set *content.Set) error {
	u, ok := m.unit(unitID)
	if !ok {
		return &protocol.Refusal{Reason: protocol.ReasonUnknownUnit, ResourceID: unitID,
			Text: fmt.Sprintf("единицы %s в партии нет", unitID)}
	}
	label := mapfmt.Labeled(u.Name, u.ID)
	st, ok := set.StockType(u.Type)
	if !ok {
		// Тип, которого нет в наборе, — поломка сервера, а не отказ игроку:
		// расстановка проверялась об этот же набор при старте.
		return fmt.Errorf("match: единица %s: тип %s в наборе контента не объявлен", label, u.Type)
	}
	if st.Controls == nil {
		return &protocol.Refusal{Reason: protocol.ReasonNoControls, ResourceID: unitID,
			Text: fmt.Sprintf("у единицы %s (тип %s) нет органов управления", label, u.Type)}
	}
	if !c.Reverser.Known() {
		return &protocol.Refusal{Reason: protocol.ReasonUnknownReverser, ResourceID: unitID,
			Text: fmt.Sprintf("реверсор %q: знаю %q, %q и %q",
				c.Reverser, ReverserNeutral, ReverserForward, ReverserReverse)}
	}
	if c.Traction < 0 || c.Traction > st.Controls.TractionNotches {
		return &protocol.Refusal{Reason: protocol.ReasonNotchOutOfRange, ResourceID: unitID,
			Text: fmt.Sprintf("ступень тяги %d, у типа %s их %d",
				c.Traction, u.Type, st.Controls.TractionNotches)}
	}
	if c.Brake < 0 || c.Brake > st.Controls.BrakeNotches {
		return &protocol.Refusal{Reason: protocol.ReasonNotchOutOfRange, ResourceID: unitID,
			Text: fmt.Sprintf("ступень торможения %d, у типа %s их %d",
				c.Brake, u.Type, st.Controls.BrakeNotches)}
	}
	// ПНЕВМАТИКА: кран машиниста и кран вспомогательного тормоза. Проверяется
	// ПРОТИВ МАШИНЫ, а не вообще: команда крану, которого на этой машине нет, —
	// отказ, а не молчаливый ноль. Тормозная система у машин разная, и пульт
	// обязан узнать об этом отказом, а не тем, что рукоятка не слушается.
	air, hasAir := st.AirBrake()
	if c.Handle != "" && !hasAir {
		return &protocol.Refusal{Reason: protocol.ReasonNoControls, ResourceID: unitID,
			Text: fmt.Sprintf("у единицы %s (тип %s) нет тормозной магистрали — крана машиниста тоже нет",
				label, u.Type)}
	}
	if hasAir {
		if !c.Handle.Known() {
			return &protocol.Refusal{Reason: protocol.ReasonUnknownHandle, ResourceID: unitID,
				Text: fmt.Sprintf("положение крана машиниста %q: знаю %v", c.Handle, brake.Handles)}
		}
		if _, err := air.SetIndependent(c.Independent); c.Independent != 0 && err != nil {
			return &protocol.Refusal{Reason: protocol.ReasonNotchOutOfRange, ResourceID: unitID,
				Text: err.Error()}
		}
	} else if c.Independent != 0 {
		return &protocol.Refusal{Reason: protocol.ReasonNoControls, ResourceID: unitID,
			Text: fmt.Sprintf("у единицы %s (тип %s) нет крана вспомогательного тормоза", label, u.Type)}
	}

	// ТЯГА ПРИ РЕВЕРСОРЕ В НУЛЕ — ОТКАЗ, А НЕ ТИХИЙ НОЛЬ.
	//
	// Это не наше правило игры, а устройство машины: при нулевом реверсоре цепь
	// тяги не собирается, и ставить ступень некуда. Принять команду и молча
	// оставить тягу нулевой значило бы показать машинисту выставленный
	// контроллер при неедущей машине — то есть заставить его искать
	// неисправность там, где её нет.
	if c.Traction > 0 && c.Reverser == ReverserNeutral {
		return &protocol.Refusal{Reason: protocol.ReasonTractionWithoutReverser, ResourceID: unitID,
			Text: fmt.Sprintf("ступень тяги %d при реверсоре в нуле: цепь тяги не собирается",
				c.Traction)}
	}
	if m.Controls == nil {
		m.Controls = map[string]Controls{}
	}
	m.Controls[unitID] = c
	return nil
}

// ControlsOf — положение органов единицы.
//
// Второе возвращаемое значение отличает «машина без органов» от «органы в
// нуле»: у вагона рукояток нет вовсе, и отдавать по нему нули значило бы
// говорить, что у него контроллер на нулевой позиции.
//
// Приёмник ЗНАЧЕНИЕМ, а не указателем: читатели держат СНИМОК партии (копию), и
// метод-геттер, требующий указателя, заставлял бы их заводить переменную ради
// чтения. Правит состояние только SetControls, и он указатель.
func (m Match) ControlsOf(unitID string) (Controls, bool) {
	c, ok := m.Controls[unitID]
	return c, ok
}

// unit ищет единицу по идентификатору.
//
// Линейным поиском, и это замер, а не небрежность: единиц в партии сегодня
// одна, в обозримом будущем — десятки, а карта на десяток элементов проигрывает
// срезу и по памяти, и по времени. Заводить индекс раньше замера значило бы
// оптимизировать вслепую.
func (m Match) unit(id string) (Unit, bool) {
	for _, u := range m.Units {
		if u.ID == id {
			return u, true
		}
	}
	return Unit{}, false
}
