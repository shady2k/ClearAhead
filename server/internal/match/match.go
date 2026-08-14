// Package match — партия: то, что происходит в мире, пока в него играют.
//
// # Почему это не карта и не набор контента
//
// Три слоя, и путать их дорого (спека строительных транзакций §10):
//
//	карта    — инфраструктура: топология, геометрия, рецепт рельефа. Ревизия.
//	набор    — какие машины бывают: паспорта и виды. Общий на сервер.
//	ПАРТИЯ   — что где стоит и что с ним происходит. Здесь.
//
// Решение владельца 2026-08-13: «Положение локомотива — это точно состояние
// сессии». Довод, который его подтверждает числами: положи расстановку в карту,
// и переставить локомотив на соседний путь можно будет только новой ревизией
// региона — а ревизия стоит в адресе сети и каталога, то есть смена места
// машины обесценила бы клиентам кэш всего региона. Двух партий на одной карте
// при этом не бывает вовсе.
//
// # Партия сегодня одна и безымянна в адресе
//
// Живое состояние отдаётся на /regions/{region}/live, а не на
// /matches/{id}/live, потому что каталога партий не существует и спросить у
// сервера «какие есть партии» сегодня негде. Но идентификатор партии ЕДЕТ В
// ТЕЛЕ ответа: сущность названа раньше, чем адресована, и в тот день, когда
// партий станет две, клиент уже знает слово, а не узнаёт о нём вместе со сменой
// адреса.
//
// Слово «сессия» под это НЕ занимается: в проекте оно означает подключение
// клиента (ключ идемпотентности «session_id, actor_id, command_id», hello с
// session_id при переподключении). Партия и подключение — разные вещи, и одно
// слово на две сущности в соседних строках протокола уже стоило бы разбора.
//
// # Чего здесь нет
//
// Ни скорости, ни ускорения, ни органов управления, ни времени. Ничего не
// движется: тика не существует, командовать нечем. Поля появятся вместе с тем,
// что их считает, а не нулями заранее — ноль в контракте неотличим от
// «не заполнено».
package match

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/shady2k/ClearAhead/server/internal/content"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/track"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// FormatVersion — версия формата файла расстановки.
const FormatVersion = 1

// MaxDocumentBytes — потолок файла расстановки.
const MaxDocumentBytes = 1 << 20

// Unit — подвижная единица в партии: экземпляр, а не тип.
type Unit struct {
	ID string `json:"id"`
	// Name — читаемая метка единицы (расстановка; решение владельца «UUIDv7
	// везде»): тождеством не является, но её клиент показывает игроку вместо
	// UUID.
	Name string `json:"name,omitempty"`
	// Type — идентификатор паспорта в наборе контента.
	Type string `json:"type"`
	// At — ТОЧКА ОТСЧЁТА единицы: середина между плоскостями автосцепок.
	//
	// Определение обязано быть одно на весь проект, потому что кандидатов
	// несколько и каждый даёт другое положение: середина машины, геометрический
	// центр кузова, первая тележка, передняя автосцепка, начало координат
	// ассета. Выбрана середина — она симметрична, не зависит от того, каким
	// концом машина повёрнута, и от неё до каждого конца ровно length/2.
	At netloc.PointU `json:"at"`
}

// Match — состояние партии.
type Match struct {
	// ID — имя партии. Сегодня одно и то же на всё время работы сервера.
	ID     string
	Region string
	Units  []Unit
}

// document — файл расстановки как он записан.
type document struct {
	FormatVersion int    `json:"format_version"`
	Region        string `json:"region"`
	Units         []Unit `json:"units"`
}

// Start собирает партию из файла начальной расстановки.
//
// Пустой путь означает партию БЕЗ подвижного состава, и это законное состояние
// мира, а не ошибка: станция без единой машины — обычное дело. Отсутствие
// ключа и отсутствие файла — разные вещи: первое молчаливо и законно, второе
// отказ.
func Start(id, path string, net *track.CompiledNetwork, set *content.Set) (*Match, error) {
	m := &Match{ID: id, Region: net.MapID}
	if path == "" {
		return m, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("match: файл расстановки: %w", err)
	}
	if len(raw) > MaxDocumentBytes {
		return nil, fmt.Errorf("match: файл расстановки больше %d байт", MaxDocumentBytes)
	}
	var doc document
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("match: разбор %s: %w", path, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("match: после документа %s есть лишние данные", path)
	}
	if doc.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("match: версия формата %d не поддерживается, ожидается %d",
			doc.FormatVersion, FormatVersion)
	}
	// Расстановка называет регион, хотя сервер и так знает свой: несовпадение
	// значит, что подсунули расстановку от другой карты, и элементы в ней
	// случайно совпадут по именам скорее, чем не совпадут.
	if doc.Region != net.MapID {
		return nil, fmt.Errorf("match: расстановка для региона %s, а мир поднят с %s",
			doc.Region, net.MapID)
	}
	if err := m.place(doc.Units, net, set); err != nil {
		return nil, err
	}
	return m, nil
}

// place проверяет расстановку и укладывает единицы.
//
// Проверяется всё, что можно проверить при загрузке: испорченная расстановка
// обязана не подняться, а не доехать до клиента полусобранной.
func (m *Match) place(list []Unit, net *track.CompiledNetwork, set *content.Set) error {
	seen := map[string]bool{}
	// occupied — занятые отрезки по элементам, в s. Нужен для запрета
	// наложения: две машины в одном месте — не «спорная ситуация», а карта,
	// которую нельзя поднять.
	occupied := map[string][][2]units.Distance{}
	for i, u := range list {
		if err := mapfmt.ValidID("подвижная единица", u.ID); err != nil {
			return fmt.Errorf("match: единица %d: %w", i, err)
		}
		if seen[u.ID] {
			return fmt.Errorf("match: единица %s объявлена дважды", mapfmt.Labeled(u.Name, u.ID))
		}
		seen[u.ID] = true
		if err := u.At.Structural(); err != nil {
			return fmt.Errorf("match: единица %s: %w", mapfmt.Labeled(u.Name, u.ID), err)
		}
		// Направление у подвижной единицы ОБЯЗАТЕЛЬНО: пустое значение в
		// netloc означает «объект направления не имеет», и это правда для
		// платформы, но не для машины — у неё есть перёд.
		if !u.At.Direction.Directed() {
			return fmt.Errorf("match: единица %s: направление не задано; "+
				"у машины есть перёд, и пустое направление означало бы обратное", mapfmt.Labeled(u.Name, u.ID))
		}
		st, ok := set.StockType(u.Type)
		if !ok {
			return fmt.Errorf("match: единица %s: тип %s в наборе контента не объявлен", mapfmt.Labeled(u.Name, u.ID), u.Type)
		}
		el, ok := net.Elements[u.At.Element]
		if !ok {
			return fmt.Errorf("match: единица %s: элемента %s в сети нет", mapfmt.Labeled(u.Name, u.ID), u.At.Element)
		}
		if math.IsNaN(u.At.U) || math.IsInf(u.At.U, 0) || u.At.U < 0 {
			return fmt.Errorf("match: единица %s: u = %v", mapfmt.Labeled(u.Name, u.ID), u.At.U)
		}

		from, to, err := extentS(u, st, el)
		if err != nil {
			return fmt.Errorf("match: единица %s: %w", mapfmt.Labeled(u.Name, u.ID), err)
		}
		for _, busy := range occupied[u.At.Element] {
			// Полуоткрытые интервалы: касание концами наложением НЕ считается.
			// Соглашение принято раньше и не здесь (ClearAhead-5zd), но
			// применяется впервые — при целых микрометрах равенство достижимо,
			// и «какая из двух сторон границы занята» не имеет естественного
			// ответа.
			if from < busy[1] && busy[0] < to {
				return fmt.Errorf("match: единица %s накладывается на уже стоящую "+
					"на элементе %s: [%s, %s) против [%s, %s)",
					mapfmt.Labeled(u.Name, u.ID), u.At.Element, from, to, busy[0], busy[1])
			}
		}
		occupied[u.At.Element] = append(occupied[u.At.Element], [2]units.Distance{from, to})
	}
	m.Units = append([]Unit(nil), list...)
	return nil
}

// extentS считает занимаемый машиной отрезок в s и проверяет помещаемость.
//
// # Почему в s, а не вычитанием u
//
// Потому что это РАЗНЫЕ величины. u — авторская координата вдоль
// горизонтальной проекции, s — пространственная длина оси; на уклоне ось
// длиннее проекции. Машина занимает длину вдоль ОСИ, и проверка «влезает ли»,
// сделанная вычитанием метров u, при заметном уклоне разрешила бы то, что не
// влезает. Сегодня уклоны в карте нулевые и разницы нет — проверка написана
// правильно не ради сегодняшних чисел, а ради того дня, когда они появятся.
//
// # Почему не влезающая машина — отказ, а не подвинуть
//
// Правило проекта: валидатор отказывает, а не чинит. Подвинуть машину внутрь
// элемента значило бы поставить её не туда, куда сказал автор, и молча.
// Пройти же хвостом через стрелку сегодня нечем: направленный обход портов —
// это занятость веха В3, и до неё машина обязана помещаться в один элемент.
func extentS(u Unit, st content.StockType, el track.CompiledElement) (from, to units.Distance, err error) {
	uMicro, err := units.MetersToDistance(u.At.U)
	if err != nil {
		return 0, 0, fmt.Errorf("смещение u: %w", err)
	}
	if uMicro > el.LengthU {
		return 0, 0, fmt.Errorf("u = %s больше длины элемента %s (%s)", uMicro, el.ID, el.LengthU)
	}
	center, err := el.Prof.UToS(uMicro)
	if err != nil {
		return 0, 0, fmt.Errorf("перевод u в s: %w", err)
	}
	half, err := units.MetersToDistance(st.LengthM / 2)
	if err != nil {
		return 0, 0, fmt.Errorf("полудлина типа %s: %w", st.ID, err)
	}
	from, to = center-half, center+half
	if from < 0 || to > el.LengthS {
		return 0, 0, fmt.Errorf("машина типа %s длиной %.2f м, поставленная серединой на u = %.2f м, "+
			"занимает [%s, %s] элемента %s длиной %s — не помещается; "+
			"хвостом через стрелку сегодня не проходят",
			st.ID, st.LengthM, u.At.U, from, to, el.ID, el.LengthS)
	}
	return from, to, nil
}
