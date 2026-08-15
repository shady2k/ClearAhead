package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ProtocolVersion — MAJOR версия конверта канала.
//
// Версионируется КОНВЕРТ, а не каждое сообщение (вертикальный срез §6): иначе
// у клиента появляется таблица «какая версия у какого типа», и первая же
// рассинхронизация в ней становится молчаливой. Неизвестная major — явный
// отказ; неизвестные ПОЛЯ внутри известной major клиент игнорирует, и потому
// приборы машиниста не поднимают версию.
const ProtocolVersion = 1

// MaxSessionIDLength — потолок на идентификатор сессии, приходящий снаружи.
//
// Сессию выдаёт сервер, но при переподключении клиент возвращает её обратно —
// то есть это внешний вход, и на него распространяется общее правило: у строки
// из сети есть предел длины. 64 — с запасом на UUID в любой записи.
const MaxSessionIDLength = 64

// Refusal — ДОМЕННЫЙ отказ с машинной причиной: то, что уезжает в data ответа
// с кодом -32001.
//
// Заведён здесь, а не в транспорте, потому что причина отказа — часть
// контракта, а не свойство сокета. Транспорт лишь переводит её в кадр.
//
// Три поля из вертикального среза §6, и ни одно не обязательно, кроме первого:
// reason называет ПРАВИЛО, которое не выполнено, resource_id — за что шла
// борьба, held_by — кто держит. У отказа без держателя два последних поля
// пусты, и это законно: не всякий отказ есть конфликт.
type Refusal struct {
	Reason     string `json:"reason"`
	ResourceID string `json:"resource_id,omitempty"`
	HeldBy     string `json:"held_by,omitempty"`
	// Text — человеческая формулировка причины. НА ПРОВОД НЕ ЕДЕТ полем: она
	// уезжает в message ответа, где ей и место (машинное — в data).
	//
	// Заведена потому, что общая формулировка по трём полям врёт на отказах,
	// которые не являются конфликтом за ресурс: «отказано: X (ресурс A занят
	// B)» читается как захват даже там, где никто ничего не держит.
	Text string `json:"-"`
}

// Error делает отказ ошибкой Go: обработчик возвращает его как error, а
// транспорт достаёт errors.As и кладёт в data.
//
// Текст человеческий и по-русски — его читают в логе; машинное решение
// принимается по Reason.
func (r *Refusal) Error() string {
	if r.Text != "" {
		return "отказано: " + r.Text
	}
	if r.HeldBy != "" {
		return fmt.Sprintf("отказано: %s (ресурс %s занят %s)", r.Reason, r.ResourceID, r.HeldBy)
	}
	if r.ResourceID != "" {
		return fmt.Sprintf("отказано: %s (ресурс %s)", r.Reason, r.ResourceID)
	}
	return "отказано: " + r.Reason
}

// Причины отказа. Список ЗАКРЫТ на стороне сервера: клиент вправе не знать
// новую причину и обязан показать её как есть, но выдумывать причины на ходу
// нельзя — reason читается машиной, а машина сравнивает строку.
const (
	// ReasonUnsupportedProtocol — major версия конверта не наша.
	ReasonUnsupportedProtocol = "unsupported_protocol_version"
	// ReasonUnknownSession — клиент назвал сессию, которой сервер не знает.
	// Это НЕ ошибка клиента: сервер перезапустили, и сессии пережить это
	// нечем. Клиент обязан поздороваться заново без session_id.
	ReasonUnknownSession = "unknown_session"
	// ReasonNotGreeted — в канал пришла команда до рукопожатия.
	ReasonNotGreeted = "not_greeted"
	// ReasonAlreadyGreeted — второе рукопожатие в одном соединении.
	ReasonAlreadyGreeted = "already_greeted"

	// Причины отказа ДОМЕННЫХ команд. Первые пять пришли с первой командой —
	// органами управления кабины (ClearAhead-6ygr).
	//
	// Почему причины домена живут в пакете провода: reason читается МАШИНОЙ, и
	// он часть контракта наравне с именами полей. Разбросав их по доменным
	// пакетам, мы получили бы список причин, которого нет целиком нигде, —
	// а договор обязан перечислить их все (contract/channel.v1.json).

	// ReasonUnknownUnit — такой единицы в партии нет.
	ReasonUnknownUnit = "unknown_unit"
	// ReasonNoControls — у машины нет органов управления (вагон).
	ReasonNoControls = "no_controls"
	// ReasonNotchOutOfRange — ступени с таким номером у этой машины нет.
	ReasonNotchOutOfRange = "notch_out_of_range"
	// ReasonUnknownReverser — неизвестное положение реверсора.
	ReasonUnknownReverser = "unknown_reverser"
	// ReasonUnknownHandle — неизвестное положение ручки крана машиниста.
	// Отдельная причина от реверсора: обе рукоятки перечислением, но перечни у
	// них разные, и общий код заставил бы клиента гадать, какую именно он не
	// угадал.
	ReasonUnknownHandle = "unknown_brake_handle"
	// ReasonTractionWithoutReverser — тяга при реверсоре в нуле: цепь тяги не
	// собирается, и это устройство машины, а не наше правило игры.
	ReasonTractionWithoutReverser = "traction_without_reverser"

	// Причины отказа команды перевода стрелки (ClearAhead-duf, первая
	// диспетчерская команда проекта).

	// ReasonUnknownTurnout — такого устройства в регионе нет.
	ReasonUnknownTurnout = "unknown_turnout"
	// ReasonUnknownTurnoutPosition — неизвестное положение остряка.
	//
	// Отдельно от ReasonUnknownReverser и ReasonUnknownHandle по той же причине,
	// по которой те разведены между собой: перечни у всех троих разные, и общий
	// код заставил бы клиента гадать, какой именно он не угадал.
	ReasonUnknownTurnoutPosition = "unknown_turnout_position"
	// ReasonTurnoutOccupied — под составом стрелка не переводится.
	//
	// ПЕРВАЯ ПРИЧИНА С ДЕРЖАТЕЛЕМ: в held_by уезжает единица, накрывающая
	// устройство. До неё поле держателя существовало в конверте отказа, но ни
	// один отказ его не заполнял — все прежние конфликтом за ресурс не были.
	ReasonTurnoutOccupied = "turnout_occupied"
)

// HelloRequest — первое сообщение клиента в канале.
//
// # Почему рукопожатие прикладное, а не «раз сокет открылся, значит договорились»
//
// Открытый сокет доказывает, что до сервера доехали байты, и ничего больше.
// Договориться надо о трёх вещах, и ни одна из них не следует из факта
// соединения: какая major версия конверта у обеих сторон, чьё это подключение
// (новое или продолжение прежнего) и с какого места клиент считает себя
// отставшим. Всё три едут здесь.
//
// # Почему session_id приходит ОТ КЛИЕНТА, а выдаёт его сервер
//
// Потому что переподключение — это не новое подключение. Разрыв на LAN
// длится секунды, а сессия несёт ключи идемпотентности: клиент, потерявший
// сокет между отправкой команды и ответом, обязан повторить команду тем же
// command_id и получить ТОТ ЖЕ ответ, а не применить её дважды. Без
// возвращаемого session_id тройка (session_id, actor_id, command_id) после
// разрыва указывает в пустоту, и повтор становится вторым применением.
//
// Пустой session_id — законное представление и означает «я здесь впервые».
// Отдельного метода join для этого нет: два метода на одно рукопожатие
// разошлись бы в тот день, когда в него добавят поле.
type HelloRequest struct {
	protocolVersion int
	sessionID       string
	lastSnapshotSeq uint64
}

func (*HelloRequest) sealed() {}

func (r *HelloRequest) native() HelloRequest { return *r }

// helloParams — представление params на проводе.
//
// protocol_version указателем, потому что ноль и отсутствие — разные вещи:
// отсутствие есть невалидное представление (клиент не назвал версию), а ноль —
// названная неподдерживаемая версия. Первое отвергает Parse как форму, второе
// отвергает обработчик как политику, и разница видна клиенту по коду ответа.
type helloParams struct {
	ProtocolVersion *int   `json:"protocol_version"`
	SessionID       string `json:"session_id"`
	LastSnapshotSeq uint64 `json:"last_snapshot_seq"`
	// CommandID объявлен НАРОЧНО, хотя рукопожатие командой не является:
	// разбор идёт с DisallowUnknownFields, и клиент, приславший command_id
	// заодно со всеми запросами, получил бы отказ формы. Поле принимается и
	// игнорируется — ключ идемпотентности транспорт читает сам (rpc.Frame).
	CommandID string `json:"command_id,omitempty"`
}

// Parse разбирает params рукопожатия.
//
// СТРОГО: неизвестное поле — отказ, а не молчаливое пропускание. Это общее
// правило проекта («валидатор отказывает, а не чинит»), и на рукопожатии оно
// стоит дороже обычного: клиент, приславший protocol_versoin с опечаткой,
// иначе получил бы отказ по версии вместо отказа по форме и искал бы ошибку не
// там.
func (r *HelloRequest) Parse(in Input) error {
	if len(in.Path) != 0 {
		return fmt.Errorf("protocol: рукопожатие не адресуется сегментами пути")
	}
	if len(in.Body) == 0 {
		return fmt.Errorf("protocol: у рукопожатия обязаны быть params с protocol_version")
	}
	var p helloParams
	dec := json.NewDecoder(bytes.NewReader(in.Body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("protocol: разбор рукопожатия: %w", err)
	}
	if dec.More() {
		return fmt.Errorf("protocol: после params рукопожатия есть лишние данные")
	}
	if p.ProtocolVersion == nil {
		return fmt.Errorf("protocol: рукопожатие без protocol_version")
	}
	if len(p.SessionID) > MaxSessionIDLength {
		return fmt.Errorf("protocol: session_id длиннее %d символов", MaxSessionIDLength)
	}
	r.protocolVersion = *p.ProtocolVersion
	r.sessionID = p.SessionID
	r.lastSnapshotSeq = p.LastSnapshotSeq
	return nil
}

// ProtocolVersion — названная клиентом major версия конверта.
func (r HelloRequest) ProtocolVersion() int { return r.protocolVersion }

// SessionID — сессия, которую клиент продолжает. Пусто — клиент здесь впервые.
func (r HelloRequest) SessionID() string { return r.sessionID }

// LastSnapshotSeq — последний снапшот, который клиент видел.
//
// Сервер сегодня отвечает на рукопожатие ПОЛНЫМ снапшотом независимо от этого
// числа, и потому оно не влияет ни на что. Оно принимается всё равно, и это не
// задел впрок: число говорит серверу, СКОЛЬКО клиент пропустил, а это первое,
// что понадобится померить в тот день, когда снапшоты станут разностными. Поле,
// которого нет, померить нечем задним числом.
func (r HelloRequest) LastSnapshotSeq() uint64 { return r.lastSnapshotSeq }
