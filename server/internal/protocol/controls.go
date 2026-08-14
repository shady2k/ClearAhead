package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// ControlsRequest — ПЕРВАЯ ДОМЕННАЯ КОМАНДА проекта: положение органов
// управления кабины.
//
// До неё канал команд был закончен, не назвав ни одной команды (ClearAhead-wa51,
// и это был его объявленный критерий). Здесь канал получает первого настоящего
// потребителя, и форма команды выбрана так, чтобы у неё не было двусмысленных
// представлений.
//
// # Ставится ПОЛОЖЕНИЕ ВСЕХ ОРГАНОВ разом
//
// Частичная правка («поставь только тягу») потребовала бы отличать «не трогать»
// от «поставить ноль» — то есть отличать отсутствующее поле от нулевого, а это
// ровно та двусмысленность, которую проект запрещает всюду. Полное положение
// кабины идемпотентно само по себе: повтор ставит то же самое, и ключ
// идемпотентности защищает не от двойного применения, а от двойного ОТВЕТА.
//
// # Числа — указателями
//
// Ноль ступени тяги и отсутствие поля — РАЗНЫЕ сообщения: первое значит
// «контроллер на нуле», второе — «клиент не назвал тягу». Первое законно,
// второе отказ формы, и различить их можно только указателем.
type ControlsRequest struct {
	unit     string
	traction int
	brake    int
	reverser string
}

func (*ControlsRequest) sealed() {}

func (r *ControlsRequest) native() ControlsRequest { return *r }

// controlsParams — представление params на проводе.
type controlsParams struct {
	// Unit — какой машиной управляем. Обязателен и сегодня, когда она одна:
	// команда без адресата стала бы командой «той единственной», и второй
	// локомотив превратил бы её в лотерею.
	Unit     string  `json:"unit"`
	Traction *int    `json:"traction"`
	Brake    *int    `json:"brake"`
	Reverser *string `json:"reverser"`
	// CommandID объявлен, чтобы строгий разбор не отверг ключ идемпотентности:
	// его читает транспорт (rpc.Frame), а не обработчик.
	CommandID string `json:"command_id,omitempty"`
}

// Parse разбирает и проверяет params команды.
//
// ЗДЕСЬ ПРОВЕРЯЕТСЯ ФОРМА, А НЕ ПРЕДЕЛЫ. Сколько у машины ступеней — свойство
// её паспорта, и знать его пакету протокола неоткуда: пределы проверяет тот,
// у кого есть набор контента (match.SetControls). Отрицательная ступень при
// этом отвергается здесь: она невалидна у ЛЮБОЙ машины, и пропускать её вглубь
// значило бы носить заведомо испорченное значение через два слоя.
func (r *ControlsRequest) Parse(in Input) error {
	if len(in.Path) != 0 {
		return fmt.Errorf("protocol: команда органов управления не адресуется сегментами пути")
	}
	if len(in.Body) == 0 {
		return fmt.Errorf("protocol: у команды органов управления обязаны быть params")
	}
	var p controlsParams
	dec := json.NewDecoder(bytes.NewReader(in.Body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("protocol: разбор команды органов управления: %w", err)
	}
	if dec.More() {
		return fmt.Errorf("protocol: после params команды есть лишние данные")
	}
	if err := mapfmt.ValidID("подвижная единица", p.Unit); err != nil {
		return fmt.Errorf("protocol: %w", err)
	}
	if p.Traction == nil || p.Brake == nil || p.Reverser == nil {
		return fmt.Errorf("protocol: команда обязана назвать ВСЕ органы " +
			"(traction, brake, reverser): частичная правка не отличима от постановки нуля")
	}
	if *p.Traction < 0 {
		return fmt.Errorf("protocol: ступень тяги %d отрицательна", *p.Traction)
	}
	if *p.Brake < 0 {
		return fmt.Errorf("protocol: ступень торможения %d отрицательна", *p.Brake)
	}
	if *p.Reverser == "" {
		return fmt.Errorf("protocol: положение реверсора пусто")
	}
	r.unit, r.traction, r.brake, r.reverser = p.Unit, *p.Traction, *p.Brake, *p.Reverser
	return nil
}

// Unit — какой машиной управляют.
func (r ControlsRequest) Unit() string { return r.unit }

// Traction — ступень контроллера тяги.
func (r ControlsRequest) Traction() int { return r.traction }

// Brake — ступень служебного торможения.
func (r ControlsRequest) Brake() int { return r.brake }

// Reverser — положение реверсора как строка провода.
//
// Строкой, а не доменным типом: пакет протокола описывает ПРОВОД и намеренно не
// знает домена. Значение проверит тот, кто им распоряжается (match.Reverser),
// и отказ по неизвестному положению придёт оттуда — вместе с перечнем
// известных.
func (r ControlsRequest) Reverser() string { return r.reverser }
