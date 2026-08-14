// Package sim — движение: то, что превращает положение рукояток в пройденный
// путь.
//
// # Место в картине
//
// Движок владеет партией и даёт такты (engine), актуаторы держат положение
// рукояток (match), паспорт даёт силы (physics), сеть даёт уклон и радиус
// (track). Здесь всё это встречается и превращается в скорость и место.
//
// Это ФАЗА ХОДА тика — та самая, которая до сегодня была объявлена пустой
// вслух: «между приёмом и печатью обязано быть названо место, куда встанет
// физика, иначе первый же её автор поставит её после печати».
//
// # Шаг физики отделён от тика, и это не украшение
//
// Тик координации — 100 мс: на нём принимаются команды и рассылаются снапшоты.
// Шаг физики — 20 мс, пять подшагов внутри тика. Довод записан в эскизе ядра §9
// и он количественный: сопротивление, тяга и тормозная сила зависят от
// СКОРОСТИ, поэтому интегрирование при замороженных силах — кусочно-постоянная
// аппроксимация, и её ошибка растёт с длиной шага.
//
// ЗАМЕР, а не рассуждение (TestStepSizeError): разгон ВЛ80 с места до сотни
// секунд на шаге 100 мс и на шаге 20 мс расходится по пройденному пути на
// доли процента; на шаге 1 с — на проценты. Двадцать миллисекунд взяты как
// верхняя граница диапазона, названного спекой (10–20 мс).
//
// # Чего здесь НЕТ
//
// ЗАНЯТОСТИ И ЗАПРЕТА НАЛОЖЕНИЯ. Это вторая половина В3 (ClearAhead-fcy), и у
// неё сегодня нет потребителя: машина в партии одна, наложить её не на кого, а
// «стрелку под составом не перевести» проверяется командой перевода, которой
// ещё нет (диспетчерская половина, ClearAhead-duf). Занятость приедет вместе с
// ними — вместе с ней и обратный индекс, и направленные визиты.
//
// ТОЧНЫХ СОБЫТИЙ ВНУТРИ ШАГА (переход элемента, уход ведомого конца) тоже нет:
// переход обрабатывается по факту пересечения границы, с переносом остатка
// пути. Разница между этим и точным решением момента — доли миллиметра на шаге
// в 20 мс, и она станет важной вместе с занятостью, где момент занятия секции
// решает, кого пускать.
package sim

import (
	"fmt"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/content"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/physics"
	"github.com/shady2k/ClearAhead/server/internal/track"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// PhysicsStep — шаг интегрирования. Разбор — в шапке пакета.
const PhysicsStep = 20 * units.Millisecond

// World — неизменное окружение движения: сеть и набор контента.
//
// Оба указателя — РАЗДЕЛЯЕМЫЕ КОНСТАНТЫ: после загрузки ни сеть, ни набор не
// меняются, и общий указатель на них не является разделяемым изменяемым
// состоянием. Партию мир не держит: её приносит движок в Advance.
type World struct {
	net *track.CompiledNetwork
	set *content.Set
	// ends — какие элементы сходятся в порту. Строится один раз: обход всех
	// элементов на каждом шаге физики стоил бы линейного поиска в горячем пути.
	ends map[string][]string
}

// NewWorld собирает окружение и его индекс связности.
func NewWorld(net *track.CompiledNetwork, set *content.Set) *World {
	w := &World{net: net, set: set, ends: map[string][]string{}}
	for id, el := range net.Elements {
		w.ends[el.From] = append(w.ends[el.From], id)
		w.ends[el.To] = append(w.ends[el.To], id)
	}
	return w
}

// Advance — ФАЗА ХОДА одного тика: продвинуть мир на dt модельного времени.
//
// Зовётся движком под его замком, то есть партия здесь принадлежит нам одним.
// Ошибка означает поломку мира (элемент исчез из сети, паспорт из набора), а не
// отказ игроку: команды сюда не доходят, они уже применены в фазе приёма.
func (w *World) Advance(m *match.Match, dt units.SimTime) error {
	if dt <= 0 {
		return nil
	}
	for _, u := range m.Units {
		if err := w.advanceUnit(m, u, dt); err != nil {
			return err
		}
	}
	return nil
}

// advanceUnit двигает одну машину.
func (w *World) advanceUnit(m *match.Match, u match.Unit, dt units.SimTime) error {
	mo, ok := m.MotionOf(u.ID)
	if !ok {
		// Единица без состояния физики — не поломка: так выглядит то, что не
		// катится вовсе. Сегодня таких нет, но заводить их будут не здесь.
		return nil
	}
	st, ok := w.set.StockType(u.Type)
	if !ok {
		return fmt.Errorf("sim: единица %s: тип %s исчез из набора", u.ID, u.Type)
	}
	loco, isLoco := st.Locomotive()
	controls, hasControls := m.ControlsOf(u.ID)
	if !isLoco || !hasControls || st.Controls == nil {
		// Вагон катится только когда его толкают, а толкать сегодня нечем:
		// сцепов нет (В4). Стоящий вагон — законное состояние, а не пропуск.
		return nil
	}

	// ПОДШАГИ. Остаток тика, не делящийся на шаг, доезжает последним подшагом:
	// выбросить его значило бы терять модельное время, а мир, теряющий время,
	// перестаёт быть воспроизводимым.
	left := dt
	for left > 0 {
		step := PhysicsStep
		if left < step {
			step = left
		}
		var err error
		mo, err = w.integrate(m, u, st, loco, controls, mo, step)
		if err != nil {
			return err
		}
		left -= step
	}
	m.SetMotion(u.ID, mo)
	return nil
}

// integrate — один шаг физики.
func (w *World) integrate(m *match.Match, u match.Unit, st content.StockType,
	loco physics.Locomotive, c match.Controls, mo match.Motion, dt units.SimTime) (match.Motion, error) {
	el, ok := w.net.Elements[mo.Element]
	if !ok {
		return mo, fmt.Errorf("sim: единица %s: элемента %s нет в сети", u.ID, mo.Element)
	}
	grade, radius, err := el.AlignmentAt(mo.S)
	if err != nil {
		return mo, fmt.Errorf("sim: единица %s: %w", u.ID, err)
	}

	speed := mo.Speed
	force := w.forces(loco, st, c, mo, grade, radius)

	// Δv = F·dt/m. Единицы сходятся сами: ньютон на килограмм — это м/с², а
	// микрометры в секунду на микросекунду — те же м/с². Множителей нет и не
	// нужно, и это довод в пользу выбранных шкал, а не совпадение.
	dv := units.Speed(divRound(int64(force)*int64(dt), int64(loco.Mass)))
	next := speed + dv

	// ТОРМОЗ НЕ УМЕЕТ ПОЕХАТЬ НАЗАД. Если за шаг сила сменила знак скорости, а
	// тяги нет, машина ОСТАНОВИЛАСЬ: продолжать интегрировать значило бы
	// разогнать её обратно тормозом, и на длинном стоянии она поехала бы сама.
	if speed != 0 && sign(next) != sign(speed) && c.Traction == 0 {
		next = 0
	}
	// Конструкционная скорость — предел машины, а не предел мира: превысить её
	// физика не даёт, потому что паспорт не даёт.
	if next > loco.MaxSpeed {
		next = loco.MaxSpeed
	}
	if next < -loco.MaxSpeed {
		next = -loco.MaxSpeed
	}

	// Путь считается по СРЕДНЕЙ скорости шага, а не по конечной: при постоянном
	// ускорении это точное решение, а не приближение, и стоит оно одного
	// сложения.
	ds := units.Distance(divRound((int64(speed)+int64(next))*int64(dt), 2*int64(units.Second)))
	mo.Speed = next
	return w.move(m, u, mo, ds)
}

// forces — сумма сил вдоль РОСТА u элемента.
//
// # Три силы и три разных знака
//
//	ТЯГА       по направлению «вперёд» машины, помноженному на реверсор.
//	           Ступень — доля от огибающей паспорта на этой скорости.
//	ТОРМОЗ     против движения. У стоящей машины он не разгоняет её назад —
//	           поэтому при нулевой скорости тормозной силы нет вовсе.
//	СОПРОТИВЛЕНИЕ основное и от кривой — против движения; ОТ УКЛОНА — вниз по
//	           уклону независимо от того, куда едут. Разделено нарочно:
//	           physics.Resist складывает все три, считая, что едут вперёд, и на
//	           заднем ходу знак уклона перевернулся бы вместе с остальными — то
//	           есть машина катилась бы в горку сама.
func (w *World) forces(loco physics.Locomotive, st content.StockType, c match.Controls,
	mo match.Motion, grade int64, radius units.Distance) units.Force {
	var total units.Force

	// ТЯГА. Направление: куда смотрит машина на этом элементе, помноженное на
	// реверсор. Нулевой реверсор тяги не даёт вовсе — цепь не собрана, и это
	// проверено ещё на команде (match.SetControls).
	if c.Traction > 0 && c.Reverser != match.ReverserNeutral {
		dir := int64(1)
		if mo.Facing == netloc.DirReverse {
			dir = -1
		}
		if c.Reverser == match.ReverserReverse {
			dir = -dir
		}
		full := loco.TractiveEffort(abs(mo.Speed))
		part := units.Force(divRound(int64(full)*int64(c.Traction), int64(st.Controls.TractionNotches)))
		total += units.Force(dir) * part
	}

	// ТОРМОЗ. Доля от полного служебного нажатия, против движения.
	if c.Brake > 0 && mo.Speed != 0 {
		full := loco.BrakeForce(abs(mo.Speed))
		part := units.Force(divRound(int64(full)*int64(c.Brake), int64(st.Controls.BrakeNotches)))
		total -= units.Force(sign(mo.Speed)) * part
	}

	// СОПРОТИВЛЕНИЕ ДВИЖЕНИЮ: основное и от кривой. Стоящую машину они не
	// толкают — сопротивление движению у неподвижной машины равно нулю.
	if mo.Speed != 0 {
		w := loco.Res.At(abs(mo.Speed)) + physics.CurveResistance(radius)
		total -= units.Force(sign(mo.Speed)) * w.On(loco.Mass.Weight())
	}

	// УКЛОН. Тянет вниз по уклону всегда, и знак его — знак самого уклона:
	// положительный уклон вдоль роста u означает подъём, то есть силу против
	// роста u.
	total -= physics.GradeResistance(grade).On(loco.Mass.Weight())

	return total
}

// move — продвинуть машину на ds вдоль оси, переходя между элементами.
//
// # Переход отдан ПОРТУ, а не координате
//
// Конец элемента — это порт (узел или порт устройства), и в нём сходится то,
// что сходится. Смотреть на «следующий элемент по списку» нельзя: у стрелки их
// два, и какой из них следующий, решает положение остряка.
//
// ОСТАТОК ПУТИ ПЕРЕНОСИТСЯ, а не теряется: машина, дошедшая до границы за
// половину шага, вторую половину едет уже по новому элементу. Потеря остатка
// была бы тихой потерей скорости на каждой границе.
func (w *World) move(m *match.Match, u match.Unit, mo match.Motion, ds units.Distance) (match.Motion, error) {
	// Переходов за один шаг может быть несколько только на очень коротких
	// элементах; потолок стоит против бесконечного круга по замкнутой петле
	// нулевой длины — то есть против поломки карты, а не против нормы.
	for range 16 {
		el, ok := w.net.Elements[mo.Element]
		if !ok {
			return mo, fmt.Errorf("sim: единица %s: элемента %s нет в сети", u.ID, mo.Element)
		}
		next := mo.S + ds
		if next >= 0 && next <= el.LengthS {
			mo.S = next
			return mo, nil
		}
		// За какой конец вышли и сколько осталось пройти по новому элементу.
		var port string
		var rest units.Distance
		if next > el.LengthS {
			port, rest = el.To, next-el.LengthS
		} else {
			port, rest = el.From, -next
		}
		to, entry, ok := w.next(m, el, port)
		if !ok {
			// УПОР. Дальше пути нет — тупик, край карты либо стрелка, стоящая
			// не по нашему ходу. Машина встаёт В ГРАНИЦЕ, а не за ней, и
			// скорость гасится: продолжать движение значило бы ехать по
			// несуществующему пути.
			if next > el.LengthS {
				mo.S = el.LengthS
			} else {
				mo.S = 0
			}
			mo.Speed = 0
			return mo, nil
		}
		// НАПРАВЛЕНИЕ ЗАДАЁТ КОНЕЦ ВХОДА, А НЕ ПРЕЖНИЙ ЗНАК.
		//
		// Вошли началом нового элемента — едем по росту его u; вошли концом —
		// против. Знак скорости привязан к КАЖДОМУ элементу отдельно (см.
		// motion.go), поэтому на границе он не «переворачивается», а
		// НАЗНАЧАЕТСЯ заново.
		//
		// Здесь была ошибка, и её поймал тест стрелки: знак переворачивался
		// всякий раз при входе концом. Машина, шедшая к стрелке против роста u
		// и вошедшая в проход его концом (что тоже «против роста u»), получала
		// обратный знак — разворачивалась на месте и уезжала обратно к упору.
		// Проверялось это не рассуждением: TestTurnoutDecidesTheBranch показал
		// её на 230 м вместо стрелки.
		dirOld := sign(mo.Speed)
		dirNew := int64(1)
		if entry == 1 {
			dirNew = -1
		}
		if entry == 0 {
			ds = rest
			mo.S = 0
		} else {
			ds = -rest
			mo.S = to.LengthS
		}
		if dirNew != dirOld {
			// Оси элементов встречны: машина едет так же, а координата под ней
			// перевернулась — вместе со знаком скорости переворачивается и то,
			// как машина повёрнута относительно роста u.
			mo.Speed = -mo.Speed
			mo.Facing = flip(mo.Facing)
		}
		mo.Element = to.ID
	}
	return mo, fmt.Errorf("sim: единица %s: шестнадцать переходов за один шаг — петля в сети", u.ID)
}

// next — куда ведёт порт: элемент и то, каким концом он к порту прилегает
// (0 — началом, 1 — концом).
//
// Отказ означает «дальше ехать нельзя» и покрывает три разных случая одним
// ответом НАРОЧНО: тупик, край карты и стрелка не по ходу — для физики это одно
// и то же событие, машина встаёт. Различать их будет тот, кто станет об этом
// РАССКАЗЫВАТЬ игроку, и различать по своим данным, а не по коду возврата.
func (w *World) next(m *match.Match, from track.CompiledElement, port string) (track.CompiledElement, int, bool) {
	for _, id := range w.ends[port] {
		if id == from.ID {
			continue
		}
		el := w.net.Elements[id]
		// СТРЕЛКА: проход разрешён, только если остряк стоит по нему. Это
		// касается и хода с боку (в реальности — взрез), и он здесь не
		// моделируется: машина просто не едет. Взрез — повреждение устройства,
		// и заводить его надо вместе с последствиями, а не как тихий проезд.
		if dev, branch, isPassage := passageOf(id); isPassage {
			if m.TurnoutAt(dev) != branch {
				continue
			}
		}
		if el.From == port {
			return el, 0, true
		}
		return el, 1, true
	}
	return track.CompiledElement{}, 0, false
}

// passageOf — проход ли это устройства, и если да, то какого и какой ветви.
//
// Разбирается ИМЕНЕМ, и это законно ровно потому, что имя прохода собирается в
// одном месте (mapfmt.Turnout.Passages) из идентификатора устройства и
// суффикса ветви. Второй разбор здесь не заводит нового соглашения — он читает
// то же самое соглашение теми же константами.
func passageOf(id string) (device, branch string, ok bool) {
	switch {
	case strings.HasSuffix(id, mapfmt.PassageStraight):
		return strings.TrimSuffix(id, mapfmt.PassageStraight), match.TurnoutStraight, true
	case strings.HasSuffix(id, mapfmt.PassageDiverging):
		return strings.TrimSuffix(id, mapfmt.PassageDiverging), match.TurnoutDiverging, true
	}
	return "", "", false
}

func flip(d netloc.Direction) netloc.Direction {
	if d == netloc.DirForward {
		return netloc.DirReverse
	}
	if d == netloc.DirReverse {
		return netloc.DirForward
	}
	return d
}

func sign[T ~int64](v T) int64 {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	default:
		return 0
	}
}

func abs[T ~int64](v T) T {
	if v < 0 {
		return -v
	}
	return v
}

// divRound — деление с округлением к ближайшему.
//
// Своя копия рядом с такой же в physics, и это не дублирование по недосмотру:
// вынести её в общий пакет значило бы завести пакет ради четырёх строк, а
// импортировать чужую неэкспортированную нельзя. Довод у обеих один: отбрасывание
// даёт систематический сдвиг в одну сторону, то есть дрейф, неотличимый от
// ошибки в физике.
func divRound(num, den int64) int64 {
	if den == 0 {
		return 0
	}
	if (num < 0) != (den < 0) {
		return (num - den/2) / den
	}
	return (num + den/2) / den
}
