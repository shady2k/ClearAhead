package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// TurnoutRequest — ПЕРВАЯ ДИСПЕТЧЕРСКАЯ КОМАНДА проекта: перевод стрелки.
//
// # Положение называется, а не переключается
//
// Соблазн — команда «переключи» без параметра: игрок и вправду щёлкает одной
// клавишей. Она отвергнута, и довод тот же, что у полного положения кабины:
// команда без называния результата НЕ ИДЕМПОТЕНТНА. Повтор, доехавший вторым
// после разрыва, вернул бы остряк обратно — то есть ключ идемпотентности защищал
// бы от двойного ответа, а мир всё равно оказался бы в другом состоянии, чем
// думает игрок.
//
// «Переключить» остаётся у клиента: он знает текущее положение из снапшота и
// называет противоположное. Мир при этом принимает только то, что названо.
//
// # Механизм в команде не участвует
//
// Ни ручной перевод, ни электропривод в params не приезжают: чем оборудована
// стрелка — свойство КАРТЫ, и клиент, называющий его, называл бы факт о мире.
// Сервер знает его сам (track.CompiledDevice.Drive).
type TurnoutRequest struct {
	turnout  string
	position string
}

func (*TurnoutRequest) sealed() {}

func (r *TurnoutRequest) native() TurnoutRequest { return *r }

// turnoutParams — представление params на проводе.
type turnoutParams struct {
	// Turnout — какую стрелку переводим. Обязательна: команда без адресата
	// стала бы командой «той единственной», а стрелок на станции уже две.
	Turnout string `json:"turnout"`
	// Position — куда: "straight" | "diverging". Указателем, чтобы отличить
	// отсутствие поля от пустой строки: первое — забытое поле, второе —
	// названное неизвестное положение, и отказы у них разные.
	Position *string `json:"position"`
	// CommandID объявлен, чтобы строгий разбор не отверг ключ идемпотентности:
	// его читает транспорт (rpc.Frame), а не обработчик.
	CommandID string `json:"command_id,omitempty"`
}

// Parse разбирает и проверяет params команды.
//
// ЗДЕСЬ ПРОВЕРЯЕТСЯ ФОРМА, А НЕ МИР. Существует ли такая стрелка, свободна ли
// она и какое положение у неё законно — вопросы к состоянию партии, и отвечает
// на них match.SetTurnout. Перечень положений здесь не повторяется НАРОЧНО:
// вторая копия перечня разошлась бы с первой, а отказ по неизвестному положению
// обязан прийти оттуда, где этот перечень живёт.
func (r *TurnoutRequest) Parse(in Input) error {
	if len(in.Path) != 0 {
		return fmt.Errorf("protocol: команда перевода стрелки не адресуется сегментами пути")
	}
	if len(in.Body) == 0 {
		return fmt.Errorf("protocol: у команды перевода стрелки обязаны быть params")
	}
	var p turnoutParams
	dec := json.NewDecoder(bytes.NewReader(in.Body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("protocol: разбор команды перевода стрелки: %w", err)
	}
	if dec.More() {
		return fmt.Errorf("protocol: после params команды есть лишние данные")
	}
	if err := mapfmt.ValidID("стрелка", p.Turnout); err != nil {
		return fmt.Errorf("protocol: %w", err)
	}
	if p.Position == nil {
		return fmt.Errorf("protocol: команда обязана назвать положение остряка: " +
			"«переключить» без называния не идемпотентно")
	}
	r.turnout, r.position = p.Turnout, *p.Position
	return nil
}

// Turnout — какую стрелку переводят.
func (r TurnoutRequest) Turnout() string { return r.turnout }

// Position — в какое положение, строкой провода. Строкой, а не доменным типом:
// пакет протокола описывает ПРОВОД и намеренно не знает домена — значение
// проверит тот, кто им распоряжается (match.SetTurnout).
func (r TurnoutRequest) Position() string { return r.position }
