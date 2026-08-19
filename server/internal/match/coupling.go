package match

// coupling.go — СЦЕПКА И РАСЦЕПКА: как сцепы становятся одним и снова двумя.
//
// # Чем узнаётся, что концы сошлись
//
// Не сравнением координат. Соблазн велик — у обоих тел есть отрезки пути, у
// отрезков концы, и «одинаковые числа» кажутся ответом, — но он неверен на
// границе элемента: голова одного тела стоит в конце одного элемента, конец
// другого начинается в нуле следующего, числа разные, а зазора нет вовсе. Ещё
// хуже у стрелки: какой элемент следующий, решает положение остряка.
//
// Поэтому смычка узнаётся ТЕМ ЖЕ МЕХАНИЗМОМ, КОТОРЫМ ТЕЛО ЕДЕТ: отрезок
// наращивается на допуск смычки в сторону нужного конца (track.Span.GrowA — та
// же функция, что двигает состав), и если наросшее наложилось на чужой отрезок,
// значит там стоит сосед. Обход портов, положение остряков и край карты
// учитываются даром — они уже учтены внутри.
//
// # Сцепка на ходу — это и есть автосцепка
//
// Здесь стояло правило «сцепляют только стоящие», и оно ушло 2026-08-19 по
// решению владельца: «автосцепка на ходу». Правило было не про мир, а про
// ИГРОКА — про то, можно ли ему бить состав ходом, — и потому переехало туда,
// где живут правила о действиях человека (command.Couple). Мир же от скорости
// не зависит: СА-3 цепляется от удара, и цепляется всегда.
//
// ПОРОГА СКОРОСТИ НЕТ И НЕ ЗАВОДИТСЯ. Настоящая автосцепка выше нескольких
// километров в час цепляется И ЛОМАЕТ — а повреждения это модель, которой у нас
// нет. Порог, введённый раньше неё, запрещал бы то, чего мир всё равно не
// умеет наказать, и был бы числом, взятым с потолка.
//
// # Количество движения сохраняется
//
// Сцепка — это удар, и после него два тела идут одним. Скорость получившегося
// сцепа есть (m₁v₁ + m₂v₂)/(m₁+m₂), и массы поэтому приходят аргументом: их
// знает тот, у кого паспорта единиц, а у партии их нет. Удар при этом
// АБСОЛЮТНО НЕУПРУГИЙ, и это не упрощение из лени: сцепленные тела не
// разъезжаются по определению сцепки, а поглощённая энергия ушла бы в
// поглощающие аппараты — то есть в ту же модель повреждений, которой нет.

import (
	"fmt"

	"github.com/shady2k/ClearAhead/server/internal/track"
	"github.com/shady2k/ClearAhead/server/internal/units"
	"github.com/shady2k/ClearAhead/server/internal/uuidv7"
)

// CouplingGap — допуск смычки: насколько далеко ищется сосед.
//
// Миллиметр, и число выбрано с двух сторон. Снизу: тело, подъехавшее к соседу,
// останавливается делением пополам с точностью до микрометра (sim.slide), то
// есть зазор после подъезда — единицы микрометров, и ноль требовать нельзя.
// Сверху: миллиметр заведомо меньше любого зазора, который человек назвал бы
// зазором, — при нём автосцепки уже соприкасаются.
const CouplingGap = units.Millimeter

// End of consist as a probe direction: конец A наращивается GrowA, конец B — GrowB.

// Couple — сцепить два сцепа в один.
//
// Возвращает получившийся сцеп. Прежние два из партии удаляются: сцеп — это
// связь, и после сцепки прежних связей не существует, а не «они остались, но
// пустые».
//
// # Почему новый идентификатор приходит аргументом
//
// Потому что его выдаёт тот, кто знает, откуда берутся имена: команда несёт
// свой идентификатор, а домен не ходит за случайностью. Веха просит именно
// этого — «сцепка создаёт новый ConsistID»; взять имя одного из сцепляемых
// значило бы, что один состав «поглотил» другой, хотя получилось третье тело.
func (m *Match) Couple(net *track.CompiledNetwork, aID, bID, newID string,
	massA, massB units.Mass) (Consist, error) {
	if aID == bID {
		return Consist{}, fmt.Errorf("match: сцепка %s с самим собой", aID)
	}
	a, ok := m.consistByID(aID)
	if !ok {
		return Consist{}, fmt.Errorf("match: сцепа %s в партии нет", aID)
	}
	b, ok := m.consistByID(bID)
	if !ok {
		return Consist{}, fmt.Errorf("match: сцепа %s в партии нет", bID)
	}
	if newID == "" {
		return Consist{}, fmt.Errorf("match: сцепка %s и %s без имени нового сцепа", a.ID, b.ID)
	}

	aEnd, bEnd, err := m.touching(net, a, b)
	if err != nil {
		return Consist{}, err
	}
	merged, err := merge(a, b, aEnd, bEnd, newID, massA, massB)
	if err != nil {
		return Consist{}, err
	}
	if err := merged.Validate(); err != nil {
		return Consist{}, err
	}
	m.removeConsist(a.ID)
	m.removeConsist(b.ID)
	m.SetConsist(merged)
	return merged, nil
}

// AutoConsistID — имя сцепа, родившегося ОТ УДАРА, а не от команды.
//
// Выводится из имён сцепляемых и НЕ ЗАВИСИТ ОТ ИХ ПОРЯДКА: кто в кого въехал —
// вопрос к физике, а не к тождеству получившегося тела, и два прогона одного
// мира обязаны назвать его одинаково. Случайное имя развело бы две загрузки
// одной расстановки, то есть сделало бы воспроизводимость мира ложью (тот же
// довод стоит у Single).
//
// Ручная сцепка своё имя по-прежнему ПРИНОСИТ С КОМАНДОЙ: там оно нужно ради
// идемпотентности, и вывести его из состояния нельзя — повтор команды после
// разрыва обязан попасть в тот же сцеп.
func AutoConsistID(aID, bID string) string {
	if bID < aID {
		aID, bID = bID, aID
	}
	return uuidv7.Derived("consist.auto", aID, bID)
}

// touching — какими концами сошлись два сцепа.
//
// Отказ, если не сошлись ни одним: сцепка тел, стоящих порознь, — это не
// «ничего не произошло», а неверная команда, и молчать о ней нельзя.
//
// Отказ и если сошлись ДВУМЯ: так выглядит кольцо или тело нулевой длины, то
// есть поломка мира, а не выбор из двух вариантов.
func (m Match) touching(net *track.CompiledNetwork, a, b Consist) (End, End, error) {
	walk := track.NewTopology(net).At(m.TurnoutAt)
	var gotA, gotB End
	found := 0
	for _, ae := range []End{EndA, EndB} {
		probeA, err := m.probe(walk, a, ae)
		if err != nil {
			return "", "", err
		}
		if probeA == nil {
			continue
		}
		for _, be := range []End{EndA, EndB} {
			spB, ok := m.endSpan(b, be)
			if !ok {
				return "", "", fmt.Errorf("match: у сцепа %s нет состояния физики", b.ID)
			}
			if _, _, hit := probeA.Overlaps(spB); !hit {
				continue
			}
			// СПРАШИВАЕМ И С ДРУГОЙ СТОРОНЫ, и это не перестраховка. У сцепа из
			// ОДНОЙ единицы оба конца лежат на одном отрезке, и проба, попавшая в
			// него, попадает сразу в «оба конца» — сцепка тогда выглядела бы
			// кольцом. Какой конец соседа смотрит на нас, знает только его
			// собственная проба: она растёт в свою сторону.
			probeB, err := m.probe(walk, b, be)
			if err != nil {
				return "", "", err
			}
			spA, ok := m.endSpan(a, ae)
			if !ok {
				return "", "", fmt.Errorf("match: у сцепа %s нет состояния физики", a.ID)
			}
			if probeB == nil {
				continue
			}
			if _, _, back := probeB.Overlaps(spA); !back {
				continue
			}
			gotA, gotB = ae, be
			found++
		}
	}
	switch found {
	case 0:
		return "", "", fmt.Errorf("match: сцепы %s и %s не соприкасаются: "+
			"между ними больше %s, цеплять нечего", a.ID, b.ID, CouplingGap)
	case 1:
		return gotA, gotB, nil
	default:
		return "", "", fmt.Errorf("match: сцепы %s и %s соприкасаются двумя концами разом — "+
			"это кольцо, а не сцепка", a.ID, b.ID)
	}
}

// probe — отрезок КОНЦЕВОЙ ЕДИНИЦЫ, наращенный на допуск смычки в сторону
// названного конца сцепа. nil, если наращивать некуда: край карты или упор — там
// соседа быть не может.
func (m Match) probe(walk track.Walk, c Consist, end End) (track.Span, error) {
	sp, ok := m.endSpan(c, end)
	if !ok {
		return nil, fmt.Errorf("match: у сцепа %s нет состояния физики", c.ID)
	}
	mem, ok := c.endMember(end)
	if !ok {
		return nil, fmt.Errorf("match: у сцепа %s нет концевой единицы", c.ID)
	}
	// Конец СЦЕПА и конец ЕДИНИЦЫ — разные концы, если единица стоит в сцепе
	// перевёрнутой: у неё конец A смотрит в сторону конца B сцепа.
	toA := end == EndA
	if mem.Flipped {
		toA = !toA
	}
	var grown track.Span
	// ВТОРОЕ ЗНАЧЕНИЕ У НАРАЩИВАНИЯ — ОСТАТОК, а не пройденное: сколько
	// нарастить НЕ удалось (track.Span.GrowA). Ноль значит «наросло целиком»,
	// и прочитать это наоборот стоило часа отладки на составе, который стоял
	// вплотную и упорно «не соприкасался».
	var stuck units.Distance
	var err error
	if toA {
		grown, stuck, err = sp.GrowA(walk, CouplingGap)
	} else {
		grown, stuck, err = sp.GrowB(walk, CouplingGap)
	}
	if err != nil {
		return nil, fmt.Errorf("match: сцеп %s: поиск соседа: %w", c.ID, err)
	}
	if stuck == CouplingGap {
		// Не наросло ни на микрометр: край карты, упор или стрелка не по ходу.
		// Соседа там быть не может.
		return nil, nil
	}
	return grown, nil
}

// endSpan — отрезок пути КОНЦЕВОЙ единицы сцепа.
func (m Match) endSpan(c Consist, end End) (track.Span, bool) {
	mem, ok := c.endMember(end)
	if !ok {
		return nil, false
	}
	mo, ok := m.MotionOf(mem.UnitID)
	if !ok {
		return nil, false
	}
	return mo.Span, true
}

// endMember — единица, стоящая на названном конце сцепа. Члены перечислены от
// конца B к концу A, поэтому конец B — первая, конец A — последняя.
func (c Consist) endMember(end End) (Member, bool) {
	if len(c.Members) == 0 {
		return Member{}, false
	}
	if end == EndB {
		return c.Members[0], true
	}
	return c.Members[len(c.Members)-1], true
}

// merge — собрать порядок членов нового сцепа.
//
// # Что здесь считается и почему это не переставление списков
//
// Порядок членов ЕСТЬ геометрия: он читается от конца B к концу A, и всякая
// сцепка обязана дать цепочку, а не два списка рядом. Сцеп, пришедший своим
// концом A, встаёт в новую цепочку ЗАДОМ НАПЕРЁД — и вместе с порядком
// переворачиваются повороты его единиц (Flipped), иначе вагон, стоявший в своём
// составе прямо, поедет в новом задом.
func merge(a, b Consist, aEnd, bEnd End, newID string, massA, massB units.Mass) (Consist, error) {
	// Приводим к виду «конец A сцепа a смыкается с концом B сцепа b»: тогда
	// цепочка получается простым сложением списков. МАССЫ ЕДУТ ЗА СВОИМИ
	// СЦЕПАМИ: перепутать их здесь значило бы посчитать удар наоборот, и заметно
	// это стало бы только на телах разной массы — то есть не сразу.
	if aEnd == EndB {
		a, b = b, a
		aEnd, bEnd = bEnd, aEnd
		massA, massB = massB, massA
	}
	if aEnd != EndA {
		return Consist{}, fmt.Errorf("match: сцепка %s и %s: концы %q и %q не сводятся к цепочке",
			a.ID, b.ID, aEnd, bEnd)
	}
	tail := b.Members
	// ХОД СЦЕПА b В СИСТЕМЕ ОТСЧЁТА НОВОГО. Пришедший концом A встаёт в цепочку
	// задом наперёд, и вместе с порядком переворачивается ЗНАК ЕГО СКОРОСТИ: то,
	// что для него было ходом B → A, для нового сцепа стало обратным.
	speedB := b.Speed
	if bEnd == EndA {
		tail = flipMembers(b.Members)
		speedB = -speedB
	}
	speed, err := impact(a, b, a.Speed, speedB, massA, massB)
	if err != nil {
		return Consist{}, err
	}
	out := Consist{
		ID:      newID,
		Members: append(append([]Member(nil), a.Members...), tail...),
		Speed:   speed,
		// Ведущий конец наследуется от того сцепа, чей конец A стал концом A
		// нового: за ним и остаётся голова. Реверс — отдельная команда, и
		// подменять её сцепкой нельзя.
		Leading: EndA,
	}
	return out, nil
}

// impact — скорость после АБСОЛЮТНО НЕУПРУГОГО удара: (m₁v₁ + m₂v₂)/(m₁+m₂).
//
// Обе скорости уже приведены к ходу B → A НОВОГО сцепа, и приводит их merge:
// только он знает, кого при сборке цепочки перевернули.
//
// # Массы спрашиваются, ТОЛЬКО ЕСЛИ КТО-ТО ИДЁТ
//
// У двух стоящих тел количество движения нулевое при любых массах, и требовать
// их там значило бы заставить ручную сцепку ходить за паспортом ради числа,
// которое в ответе не участвует. Зато если кто-то идёт, а массы не названы, это
// ОТКАЗ, а не тихий ноль: молча остановленный удар выглядел бы как состав,
// влетевший в вагон и вставший намертво, — то есть как дефект физики.
func impact(a, b Consist, va, vb units.Speed, massA, massB units.Mass) (units.Speed, error) {
	if va == 0 && vb == 0 {
		return 0, nil
	}
	if massA <= 0 || massB <= 0 {
		return 0, fmt.Errorf("match: сцепка %s (%d кг) и %s (%d кг) на ходу: масса не названа, "+
			"количество движения не посчитать", a.ID, massA, b.ID, massB)
	}
	return units.Speed(divRound(int64(massA)*int64(va)+int64(massB)*int64(vb),
		int64(massA)+int64(massB))), nil
}

// divRound — деление с округлением к ближайшему, half away from zero.
//
// Третья копия в проекте (physics, sim, здесь), и довод у всех один: отбрасывание
// даёт систематический сдвиг в одну сторону, то есть дрейф, неотличимый от ошибки
// в физике. Своя, а не чужая, потому что чужие не экспортированы, а заводить
// общий пакет ради четырёх строк дороже, чем повторить их под тем же доводом.
func divRound(a, b int64) int64 {
	if b == 0 {
		return 0
	}
	if (a < 0) != (b < 0) {
		return (a - b/2) / b
	}
	return (a + b/2) / b
}

// flipMembers — перевернуть порядок членов и их повороты разом.
//
// Две операции ровно потому, что это одна: список читается от конца B к концу
// A, и перевернув список, мы поменяли местами концы — значит каждая единица
// теперь смотрит в сцепе в другую сторону.
func flipMembers(in []Member) []Member {
	out := make([]Member, len(in))
	for i, mem := range in {
		mem.Flipped = !mem.Flipped
		out[len(in)-1-i] = mem
	}
	return out
}

// Uncouple — расцепить сцеп ПОСЛЕ названной единицы, считая от конца B.
//
// Возвращает две части: ту, что осталась со стороны конца B, и ту, что со
// стороны конца A. Обе продолжают жить своими сцепами, и скорость наследуют
// общую — в миг расцепки они идут одинаково, а разойдутся уже своими силами.
//
// # Почему «после единицы», а не «в координате»
//
// Веха говорит «расцепка режет span по точной координате». Координата — это
// ответ на вопрос «где», а игрок задаёт вопрос «между какими»: он расцепляет
// состав между третьим и четвёртым вагоном, а не в точке 47.312 м. Координата
// при этом никуда не девается — она есть граница между отрезками названных
// единиц, и считать её отдельно значило бы завести второе написание того, что
// уже записано отрезками.
func (m *Match) Uncouple(consistID, afterUnitID, newID string) (Consist, Consist, error) {
	c, ok := m.consistByID(consistID)
	if !ok {
		return Consist{}, Consist{}, fmt.Errorf("match: сцепа %s в партии нет", consistID)
	}
	if newID == "" {
		return Consist{}, Consist{}, fmt.Errorf("match: расцепка %s без имени новой части", consistID)
	}
	at := -1
	for i, mem := range c.Members {
		if mem.UnitID == afterUnitID {
			at = i
			break
		}
	}
	if at < 0 {
		return Consist{}, Consist{}, fmt.Errorf("match: единицы %s в сцепе %s нет", afterUnitID, consistID)
	}
	if at == len(c.Members)-1 {
		return Consist{}, Consist{}, fmt.Errorf("match: единица %s — последняя в сцепе %s: "+
			"за ней расцеплять нечего", afterUnitID, consistID)
	}
	// Часть со стороны конца B сохраняет ИМЯ СЦЕПА, часть со стороны конца A
	// получает новое. Выбор произволен и потому назван: одна из частей обязана
	// остаться прежним сцепом, иначе расцепка была бы двумя рождениями и одной
	// смертью, а игрок держит в руке тот же поезд.
	head := Consist{ID: c.ID, Members: append([]Member(nil), c.Members[:at+1]...),
		Speed: c.Speed, Leading: c.Leading}
	tail := Consist{ID: newID, Members: append([]Member(nil), c.Members[at+1:]...),
		Speed: c.Speed, Leading: c.Leading}
	if err := head.Validate(); err != nil {
		return Consist{}, Consist{}, err
	}
	if err := tail.Validate(); err != nil {
		return Consist{}, Consist{}, err
	}
	m.SetConsist(head)
	m.SetConsist(tail)
	return head, tail, nil
}

// ConsistByID — сцеп по имени. Наружу его открыл канал: ответ на расцепку
// несёт ОБЕ части, и вторую в партии находят именно по имени, которое команда
// сама и назвала.
func (m Match) ConsistByID(id string) (Consist, bool) { return m.consistByID(id) }

func (m Match) consistByID(id string) (Consist, bool) {
	for _, c := range m.Consists {
		if c.ID == id {
			return c, true
		}
	}
	return Consist{}, false
}

func (m *Match) removeConsist(id string) {
	for i := range m.Consists {
		if m.Consists[i].ID == id {
			m.Consists = append(m.Consists[:i], m.Consists[i+1:]...)
			return
		}
	}
}

// NeighbourConsist — сцеп, стоящий ВПЛОТНУЮ к этому, любым из двух концов.
//
// Заведён командой сцепки и в ней же объясняется: игрок цепляется с тем, во что
// упёрся, и «с кем» в команде нет вовсе — назови клиент соседа, он назвал бы
// факт о мире, на который отвечает геометрия.
//
// Ищется перебором сцепов партии, и это не расточительство: сцепов на станции
// единицы, а команда сцепки случается раз в маневровый рейс, а не пять раз за
// тик. Появится сотня — появится и индекс, но не раньше, чем будет что мерить.
//
// Двух соседей разом эта функция не различает: если состав стоит между двумя
// другими, вернётся первый по порядку партии. Названо честно, потому что
// однажды это придётся решать — вопросом «каким концом цеплять», который
// сегодня игрок задать не может.
func (m Match) NeighbourConsist(net *track.CompiledNetwork, c Consist) (Consist, bool) {
	for _, other := range m.Consists {
		if other.ID == c.ID {
			continue
		}
		if _, _, err := m.touching(net, c, other); err == nil {
			return other, true
		}
	}
	return Consist{}, false
}
