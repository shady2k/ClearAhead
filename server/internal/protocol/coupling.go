package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// CoupleRequest — СЦЕПИТЬ: соединить свой состав с тем, что стоит вплотную.
//
// # Адресуется ЕДИНИЦЕЙ, а не двумя сцепами
//
// Игрок сидит в машине и цепляется с тем, во что упёрся. Он не знает, как
// называются сцепы, и знать не обязан: имя сцепа — внутренняя запись партии, а
// не то, что видно из кабины. Команда называет ЕДИНИЦУ, а сервер сам находит её
// сцеп и того, кто с ним сомкнулся (match.Couple).
//
// Отсюда же следует, что «с кем» в команде нет вовсе. Назови клиент соседа — он
// назвал бы ФАКТ О МИРЕ («вот эти два тела соприкасаются»), то есть решил бы за
// сервер вопрос, на который отвечает геометрия.
//
// # Имя нового сцепа приходит ОТ КЛИЕНТА
//
// Сцепка рождает третье тело, и у него новое имя (веха В4: «сцепка создаёт
// новый ConsistID»). Мяту его клиент — там же, где мятет command_id, — и по той
// же причине: команда обязана быть ИДЕМПОТЕНТНОЙ. Родись имя на сервере,
// повторная доставка той же команды после разрыва дала бы второй сцеп с другим
// именем, а ключ идемпотентности защищал бы только от второго ответа.
type CoupleRequest struct {
	unit    string
	consist string
}

func (*CoupleRequest) sealed() {}

func (r *CoupleRequest) native() CoupleRequest { return *r }

type coupleParams struct {
	// Unit — чьей сцепкой цепляем: единица, стоящая концом к соседу.
	Unit string `json:"unit"`
	// Consist — имя, которое получит новый сцеп.
	Consist string `json:"consist"`
	// CommandID объявлен, чтобы строгий разбор не отверг ключ идемпотентности:
	// его читает транспорт (rpc.Frame), а не обработчик.
	CommandID string `json:"command_id,omitempty"`
}

// Parse — форма команды. Существует ли единица, стоит ли рядом сосед и стоят ли
// оба — вопросы к состоянию партии, и отвечает на них match.Couple.
func (r *CoupleRequest) Parse(in Input) error {
	if len(in.Path) != 0 {
		return fmt.Errorf("protocol: команда сцепки не адресуется сегментами пути")
	}
	if len(in.Body) == 0 {
		return fmt.Errorf("protocol: у команды сцепки обязаны быть params")
	}
	var p coupleParams
	dec := json.NewDecoder(bytes.NewReader(in.Body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("protocol: разбор команды сцепки: %w", err)
	}
	if dec.More() {
		return fmt.Errorf("protocol: после params команды сцепки есть лишние данные")
	}
	if err := mapfmt.ValidID("подвижная единица", p.Unit); err != nil {
		return fmt.Errorf("protocol: %w", err)
	}
	if err := mapfmt.ValidID("сцеп", p.Consist); err != nil {
		return fmt.Errorf("protocol: имя нового сцепа: %w", err)
	}
	r.unit, r.consist = p.Unit, p.Consist
	return nil
}

// Unit — чьей сцепкой цепляют.
func (r CoupleRequest) Unit() string { return r.unit }

// Consist — имя нового сцепа.
func (r CoupleRequest) Consist() string { return r.consist }

// UncoupleRequest — РАСЦЕПИТЬ: разъединить состав ЗА названной единицей.
//
// # «За» — со стороны конца A сцепа
//
// Расцепка отделяет то, что стоит ДАЛЬШЕ по цепочке от конца B. Игрок при этом
// видит поезд и щёлкает по сцепке между машинами; какая из двух единиц названа,
// решает клиент, и решает однозначно: названная остаётся с тем концом, откуда
// цепочка читается (match.Uncouple).
//
// Своего имени вторая часть не выдумывает по той же причине, что и сцепка:
// повтор команды после разрыва обязан дать тот же мир, а не второй сцеп.
type UncoupleRequest struct {
	unit    string
	consist string
}

func (*UncoupleRequest) sealed() {}

func (r *UncoupleRequest) native() UncoupleRequest { return *r }

type uncoupleParams struct {
	Unit      string `json:"unit"`
	Consist   string `json:"consist"`
	CommandID string `json:"command_id,omitempty"`
}

// Parse — форма команды. Есть ли такая единица, в каком она сцепе и не
// последняя ли она в нём — вопросы к партии.
func (r *UncoupleRequest) Parse(in Input) error {
	if len(in.Path) != 0 {
		return fmt.Errorf("protocol: команда расцепки не адресуется сегментами пути")
	}
	if len(in.Body) == 0 {
		return fmt.Errorf("protocol: у команды расцепки обязаны быть params")
	}
	var p uncoupleParams
	dec := json.NewDecoder(bytes.NewReader(in.Body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("protocol: разбор команды расцепки: %w", err)
	}
	if dec.More() {
		return fmt.Errorf("protocol: после params команды расцепки есть лишние данные")
	}
	if err := mapfmt.ValidID("подвижная единица", p.Unit); err != nil {
		return fmt.Errorf("protocol: %w", err)
	}
	if err := mapfmt.ValidID("сцеп", p.Consist); err != nil {
		return fmt.Errorf("protocol: имя отцепленной части: %w", err)
	}
	r.unit, r.consist = p.Unit, p.Consist
	return nil
}

// Unit — за какой единицей расцепляют.
func (r UncoupleRequest) Unit() string { return r.unit }

// Consist — имя, которое получит отцепленная часть.
func (r UncoupleRequest) Consist() string { return r.consist }
