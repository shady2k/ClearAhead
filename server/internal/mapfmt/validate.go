package mapfmt

import (
	"fmt"
	"math"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/units"
)

// domainEpsilon — допуск на совпадение доменов трёх выравниваний. Один
// микрометр: домены задаются автором в одних и тех же метрах, расхождение
// больше округления означает опечатку.
const domainEpsilon = units.Micrometer

// PassageStraight и PassageDiverging — суффиксы ID проходов стрелки. Проход —
// адресуемый линейный элемент наравне с ребром (спека §8).
const (
	PassageStraight  = ":straight"
	PassageDiverging = ":diverging"
)

// Рукость стрелки (Turnout.Hand): в какую сторону уходит БОКОВОЙ проход, если
// смотреть от острия.
//
// Перечень закрыт, и пометка автора СВЕРЯЕТСЯ С ГЕОМЕТРИЕЙ при компиляции
// (track.turnoutDrive): по пометке строится крестовина, по геометрии — брусья и
// привод, и разойтись им нельзя.
const (
	HandLeft  = "left"
	HandRight = "right"
)

// Переводные механизмы стрелки (Turnout.Drive).
//
// Перечень ЗАКРЫТ, и третьего значения не заводится до тех пор, пока за ним не
// встанет разное поведение. Эти два различаются им уже сегодня: ручной
// переводится тем, кто до него дошёл, электрический — тем, кто до него дозвонился.
const (
	// DriveManual — ручной переводной механизм: балансир с противовесом и
	// стрелочный указатель на нём же.
	DriveManual = "manual"
	// DriveElectric — электропривод: тот же перевод, но рабочей тягой от
	// двигателя, и переводится он с пульта.
	DriveElectric = "electric"
)

// Drives — перечень механизмов списком.
//
// Заведён вместе с видами устройств (content.Device): набор контента обязан
// иметь тело для КАЖДОГО рода, и проверить это можно только по перечню. Список и
// константы выше — одно и то же знание, и второго места для него нет.
var Drives = []string{DriveManual, DriveElectric}

// KnownDrive — объявлен ли такой механизм.
func KnownDrive(kind string) bool {
	for _, k := range Drives {
		if k == kind {
			return true
		}
	}
	return false
}

// Validate проверяет форму карты. Отказывает, не чинит.
func Validate(m *Map) error {
	// map_id — адрес региона, а не тождество элемента: по нему мир и клиент
	// называют регион (worldgen.Bootstrap заводит регион строкой region :=
	// m.MapID), и решение «UUIDv7 везде» его не трогает. Форма — прежний
	// авторский идентификатор (ValidID ниже), а не UUIDv7.
	if err := ValidID("map_id", m.MapID); err != nil {
		return err
	}
	if m.MapRevision < 1 {
		return fmt.Errorf("mapfmt: map_revision должен быть положительным, получено %d", m.MapRevision)
	}

	ports, err := m.collectPorts()
	if err != nil {
		return err
	}
	elements, err := m.collectElements()
	if err != nil {
		return err
	}
	if len(elements) > MaxElements {
		return fmt.Errorf("mapfmt: элементов больше %d", MaxElements)
	}

	// Уникальность тождеств — в пределах всего региона, а не одного класса:
	// UUID живёт в одном пространстве имён карты, и элемент второго класса с
	// тем же UUID — отказ. Метки в проверке не участвуют: две одинаковые метки
	// законны и безвредны (тождество у них разное).
	if err := m.checkUniqueIDs(); err != nil {
		return err
	}

	for id, a := range m.AllAlignments() {
		if err := validateAlignments(id, a); err != nil {
			return err
		}
	}
	if err := m.validateEdgeEnds(ports); err != nil {
		return err
	}
	if err := m.validateStructures(elements); err != nil {
		return err
	}
	if err := validateGeoreference(m.Georeference); err != nil {
		return err
	}
	if err := m.validateAnchors(ports, elements); err != nil {
		return err
	}
	if err := m.validateAxisIntersections(); err != nil {
		return err
	}
	// Модули валидатора. Порядок значим: сперва структура, потом конструкция,
	// потом нормы — иначе на сломанной топологии отказ придёт не по той
	// причине. Каждый модуль живёт в своём файле и называет себя в тексте
	// отказа («отрисовка: …», «нормы: …»).
	if err := m.validateConstruction(); err != nil {
		return err
	}
	if err := validateTerrain(m.Terrain); err != nil {
		return err
	}

	if err := m.validateObjects(); err != nil {
		return err
	}
	return m.validateProfile(DefaultProfile())
}

// checkUniqueIDs — одно пространство имён UUID на всю карту: узел и ребро не
// могут делить один идентификатор. Повторы ВНУТРИ класса отвергаются раньше,
// в своих сборщиках, с более точным текстом; здесь ловится сквозной повтор.
func (m *Map) checkUniqueIDs() error {
	seen := map[string]string{}
	register := func(kind, name, id string) error {
		if prev, dup := seen[id]; dup {
			return fmt.Errorf("mapfmt: %s %s повторяет %s", kind, Labeled(name, id), prev)
		}
		seen[id] = Labeled(name, id)
		return nil
	}
	for _, n := range m.Topology.Nodes {
		if err := register("узел", n.Name, n.ID); err != nil {
			return err
		}
	}
	for _, t := range m.Topology.Turnouts {
		if err := register("стрелка", t.Name, t.ID); err != nil {
			return err
		}
	}
	for _, e := range m.Topology.Edges {
		if err := register("ребро", e.Name, e.ID); err != nil {
			return err
		}
	}
	for _, st := range m.Topology.Structures {
		if err := register("сооружение", st.Name, st.ID); err != nil {
			return err
		}
	}
	if m.Construction != nil {
		for _, tt := range m.Construction.Types {
			if err := register("тип решётки", tt.Name, tt.ID); err != nil {
				return err
			}
		}
		for _, r := range m.Construction.Runs {
			if err := register("run решётки", r.Name, r.ID); err != nil {
				return err
			}
		}
	}
	// Objects — указатель: карта без блока objects законна, и разыменование
	// здесь уронило бы её до штатной проверки формы.
	if m.Objects != nil {
		for _, b := range m.Objects.Buildings {
			if err := register("постройка", b.Name, b.ID); err != nil {
				return err
			}
		}
		for _, r := range m.Objects.Rivers {
			if err := register("река", r.Name, r.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateGeoreference проверяет блок привязки по форме. Компилятор его не
// использует (спека §4), но принимать заведомо бессмысленные числа и хранить их
// до того дня, когда они понадобятся, — способ получить неверную карту молча.
func validateGeoreference(g *Georeference) error {
	if g == nil {
		return nil
	}
	if g.Datum == "" {
		return fmt.Errorf("mapfmt: геопривязка: пустой датум")
	}
	switch g.OriginHeightKind {
	case "ellipsoidal", "orthometric":
	default:
		return fmt.Errorf("mapfmt: геопривязка: origin_height_kind должен быть ellipsoidal или orthometric, получено %q",
			g.OriginHeightKind)
	}
	if g.Origin.Lat < -90 || g.Origin.Lat > 90 {
		return fmt.Errorf("mapfmt: геопривязка: широта %v вне [-90, 90]", g.Origin.Lat)
	}
	if g.Origin.Lon < -180 || g.Origin.Lon > 180 {
		return fmt.Errorf("mapfmt: геопривязка: долгота %v вне [-180, 180]", g.Origin.Lon)
	}
	if g.XAxisAzimuthDeg < 0 || g.XAxisAzimuthDeg >= 360 {
		return fmt.Errorf("mapfmt: геопривязка: азимут %v вне [0, 360)", g.XAxisAzimuthDeg)
	}
	if !(g.GroundToGrid > 0) {
		return fmt.Errorf("mapfmt: геопривязка: ground_to_grid должен быть положительным, получено %v", g.GroundToGrid)
	}
	return nil
}

func validateAlignments(id string, a Alignments) error {
	if len(a.Horizontal) == 0 {
		return fmt.Errorf("mapfmt: %s: пустая горизонтальная цепочка", id)
	}
	nPrim := len(a.Horizontal)
	for i, p := range a.Horizontal {
		switch p.Kind {
		case "straight":
			if !(p.Length > 0) {
				return fmt.Errorf("mapfmt: %s[%d]: длина прямой должна быть положительной, получено %v", id, i, p.Length)
			}
		case "arc":
			if !(p.Radius > 0) {
				return fmt.Errorf("mapfmt: %s[%d]: радиус дуги должен быть положительным, получено %v", id, i, p.Radius)
			}
			if p.Angle == 0 || math.Abs(p.Angle) > 2*math.Pi {
				return fmt.Errorf("mapfmt: %s[%d]: угол дуги %v вне (0, 2π]", id, i, p.Angle)
			}
		default:
			return fmt.Errorf("mapfmt: %s[%d]: неизвестный примитив плана %q", id, i, p.Kind)
		}
	}
	// Длина считается единственной функцией — той же, что зовёт validateStructures.
	uH, err := horizontalLengthU(a)
	if err != nil {
		return fmt.Errorf("mapfmt: %s: %w", id, err)
	}

	if len(a.Vertical) == 0 {
		// Лимит примитивов проверяется ДО выхода: ранний return здесь делал его
		// декоративным ровно для плоских карт, то есть для всех нынешних.
		if nPrim > MaxPrimitives {
			return fmt.Errorf("mapfmt: %s: примитивов больше %d", id, MaxPrimitives)
		}
		return nil
	}
	if a.Vertical[0].Kind != "grade" {
		return fmt.Errorf("mapfmt: %s: первый элемент вертикальной цепочки обязан быть grade — он задаёт начальный уклон", id)
	}
	var uV units.Distance
	for i, p := range a.Vertical {
		nPrim++
		switch p.Kind {
		case "grade", "vertical_curve":
		default:
			return fmt.Errorf("mapfmt: %s: неизвестный примитив профиля %q", id, p.Kind)
		}
		d, err := units.MetersToDistance(p.Length)
		if err != nil || d <= 0 {
			return fmt.Errorf("mapfmt: %s: вертикаль[%d]: длина должна быть положительной, получено %v", id, i, p.Length)
		}
		uV += d
	}
	if diff := uH - uV; diff > domainEpsilon || diff < -domainEpsilon {
		return fmt.Errorf("mapfmt: %s: домены выравниваний не совпадают: план %s, профиль %s", id, uH, uV)
	}
	if nPrim > MaxPrimitives {
		return fmt.Errorf("mapfmt: %s: примитивов больше %d", id, MaxPrimitives)
	}
	return nil
}

// AllAlignments возвращает выравнивания всех линейных элементов — рёбер и
// проходов стрелок — под их ID.
func (m *Map) AllAlignments() map[string]Alignments {
	out := make(map[string]Alignments, len(m.Geometry.Edges)+2*len(m.Geometry.Turnouts))
	for id, a := range m.Geometry.Edges {
		out[id] = a
	}
	for id, tg := range m.Geometry.Turnouts {
		out[id+PassageStraight] = tg.Straight
		out[id+PassageDiverging] = tg.Diverging
	}
	return out
}

// Alignments возвращает выравнивания одного элемента.
func (m *Map) Alignments(elementID string) (Alignments, bool) {
	a, ok := m.AllAlignments()[elementID]
	return a, ok
}

// ElementIDs возвращает ID всех линейных элементов.
func (m *Map) ElementIDs() []string {
	all := m.AllAlignments()
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	return ids
}

func (m *Map) collectPorts() (map[string]Port, error) {
	ports := map[string]Port{}
	add := func(full string, p Port) error {
		if err := ValidID("порт", p.ID); err != nil {
			return err
		}
		if len(full) > MaxIDLength {
			return fmt.Errorf("mapfmt: порт %q: полное имя длиннее %d", full, MaxIDLength)
		}
		if _, dup := ports[full]; dup {
			return fmt.Errorf("mapfmt: порт %q объявлен дважды", full)
		}
		ports[full] = p
		return nil
	}
	for _, n := range m.Topology.Nodes {
		if err := checkEntity("узел", n.Name, n.ID); err != nil {
			return nil, err
		}
		for _, p := range n.Ports {
			switch p.Purpose {
			case "", "buffer_stop", "map_boundary":
			default:
				return nil, fmt.Errorf("mapfmt: порт %s.%s: неизвестное назначение %q", n.ID, p.ID, p.Purpose)
			}
			if err := add(n.ID+"."+p.ID, p); err != nil {
				return nil, err
			}
		}
	}
	for _, t := range m.Topology.Turnouts {
		if t.Hand != HandLeft && t.Hand != HandRight {
			return nil, fmt.Errorf("mapfmt: стрелка %s: рукость должна быть left или right, получено %q", Labeled(t.Name, t.ID), t.Hand)
		}
		// Механизм проверяется наравне с рукостью и по той же причине: без него
		// стрелку нельзя ни нарисовать, ни перевести, а подставленное умолчание
		// оборудовало бы станцию за автора.
		if t.Drive != DriveManual && t.Drive != DriveElectric {
			return nil, fmt.Errorf("mapfmt: стрелка %s: переводной механизм должен быть %s или %s, получено %q",
				Labeled(t.Name, t.ID), DriveManual, DriveElectric, t.Drive)
		}
		// Марка крестовины НЕ проверяется намеренно: §8 объявляет её
		// происхождением, а не ограничением, ради импорта реальных станций с
		// произвольными радиусами. Клиент строит крестовину из геометрии, а не
		// из марки.
		for _, p := range []string{t.Ports.Common, t.Ports.Straight, t.Ports.Diverging} {
			if p == "" {
				return nil, fmt.Errorf("mapfmt: стрелка %s: не заняты все три порта", Labeled(t.Name, t.ID))
			}
			if err := add(t.ID+"."+p, Port{ID: p}); err != nil {
				return nil, err
			}
		}
	}
	return ports, nil
}

func (m *Map) collectElements() (map[string]bool, error) {
	els := map[string]bool{}
	for _, e := range m.Topology.Edges {
		if err := checkEntity("ребро", e.Name, e.ID); err != nil {
			return nil, err
		}
		if err := validKind("ребро", Labeled(e.Name, e.ID), e.Kind); err != nil {
			return nil, err
		}
		if els[e.ID] {
			return nil, fmt.Errorf("mapfmt: ребро %q объявлено дважды", Labeled(e.Name, e.ID))
		}
		els[e.ID] = true
		if _, ok := m.Geometry.Edges[e.ID]; !ok {
			return nil, fmt.Errorf("mapfmt: у ребра %s нет геометрии", Labeled(e.Name, e.ID))
		}
	}
	// Устройства: идентификатор проверяется тем же правилом, что у ребра, и
	// дополнительно на повтор.
	//
	// Повтор ID делает карту неверной по построению: геометрия и концы проходов
	// лежат в map по ID, поэтому вторая стрелка затирает первую и оба прохода
	// достаются одной, а три порта другой остаются без единого прохода.
	//
	// Эта проверка дефекта НЕ ЧИНИТ — такую карту валидатор отвергал и раньше,
	// разбором стыков осей. Она его НАЗЫВАЕТ: прежний отказ звучал как
	// «порт SW.C стрелки SW: проходов 0», то есть обвинял невиновный порт
	// первой стрелки, и автор карты причину по нему не нашёл бы.
	// Недостижимость дефекта до этой проверки разобрана в duplicate_test.go.
	devs := map[string]bool{}
	for _, t := range m.Topology.Turnouts {
		if err := checkEntity("стрелка", t.Name, t.ID); err != nil {
			return nil, err
		}
		// Вид устройства проверяется наравне с видом ребра: проходы стрелки —
		// такие же адресуемые элементы сети, и берут они его отсюда.
		if err := validKind("стрелка", Labeled(t.Name, t.ID), t.Kind); err != nil {
			return nil, err
		}
		if devs[t.ID] {
			return nil, fmt.Errorf("mapfmt: стрелка %q объявлена дважды", Labeled(t.Name, t.ID))
		}
		devs[t.ID] = true
		if _, ok := m.Geometry.Turnouts[t.ID]; !ok {
			return nil, fmt.Errorf("mapfmt: у стрелки %s нет геометрии", Labeled(t.Name, t.ID))
		}
		// Проход не может столкнуться с ребром: разделитель в авторском
		// идентификаторе запрещён, поэтому ребро с именем SW:straight до сюда
		// не доходит. Проверка оставлена как страховка на случай, если правило
		// ослабят.
		for _, ps := range t.Passages() {
			if els[ps.ID] {
				return nil, fmt.Errorf("mapfmt: проход %q сталкивается с уже объявленным элементом", ps.ID)
			}
			els[ps.ID] = true
		}
	}
	for id := range m.Geometry.Edges {
		if !els[id] {
			return nil, fmt.Errorf("mapfmt: геометрия ребра %s без топологии", id)
		}
	}
	return els, nil
}

// validKind — единственное место, где записано, какие виды пути бывают.
//
// Пустое значение отвергается ОТДЕЛЬНЫМ отказом, а не сваливается в общий
// «неизвестный вид»: пустое поле означает не опечатку, а карту, написанную до
// того, как вид стал обязательным, и автору надо сказать именно это. Молчаливое
// умолчание «нет kind — значит рельсы» здесь запрещено: оно ставит на карту
// утверждение, которого автор не делал (ClearAhead-z4u).
//
// Добавление второго вида — строка в этом switch и строка в тексте отказа.
func validKind(what, id, kind string) error {
	switch kind {
	case KindRail:
		return nil
	case "":
		return fmt.Errorf("mapfmt: %s %s: не указан kind — вид элемента сети обязателен, допустимо %q",
			what, id, KindRail)
	default:
		return fmt.Errorf("mapfmt: %s %s: неизвестный вид %q, допустимо %q", what, id, kind, KindRail)
	}
}

// validateEdgeEnds требует, чтобы каждый порт был либо занят ребром, либо нёс
// назначение, делающее висящий конец законным. Безусловной связности не
// требуется: изолированное депо или вторая станция законны (спека §10.3).
func (m *Map) validateEdgeEnds(ports map[string]Port) error {
	used := map[string]int{}
	for _, e := range m.Topology.Edges {
		// Оба конца в одном порту: Incidence{Port, Element} у такого ребра
		// вырождается в один ключ, и компилятор не может представить его концы
		// раздельно. Отвергаем здесь, а не роняем компилятор ниже.
		if e.From == e.To {
			return fmt.Errorf("mapfmt: ребро %s начинается и кончается в одном порту %s", e.ID, e.From)
		}
		for _, end := range []string{e.From, e.To} {
			if _, ok := ports[end]; !ok {
				return fmt.Errorf("mapfmt: ребро %s ссылается на несуществующий порт %s", e.ID, end)
			}
			used[end]++
			// Два ребра в одном порту — это обычный стык, и именно там
			// проверяется замыкание. Три и больше — развилка, а развилка обязана
			// быть оформлена стрелкой: у неё есть длина, остряк и конфликт
			// маршрутов, которых у голого узла нет.
			if used[end] > 2 {
				return fmt.Errorf("mapfmt: порт %s обслуживает %d рёбер — развилку нужно оформить стрелкой", end, used[end])
			}
		}
	}
	for full, p := range ports {
		// Два ребра — внутренний стык, назначение не требуется. Одно ребро —
		// конец линии: без purpose это висящий конец, а он запрещён (спека §10.3).
		if used[full] > 1 {
			continue
		}
		if strings.Contains(full, ":") {
			continue
		}
		isTurnoutPort := false
		for _, t := range m.Topology.Turnouts {
			if strings.HasPrefix(full, t.ID+".") {
				isTurnoutPort = true
			}
		}
		if isTurnoutPort {
			if used[full] == 0 {
				return fmt.Errorf("mapfmt: порт стрелки %s не соединён ребром", full)
			}
			// Порт стрелки с одним ребром законен: продолжение даёт стрелка.
			continue
		}
		if p.Purpose == "" {
			return fmt.Errorf("mapfmt: висящий конец в порту %s: нужен purpose buffer_stop или map_boundary", full)
		}
	}
	return nil
}

// validateStructures — перечень видов сооружений и единственное место, где он
// записан. Здесь же видно, ПОЧЕМУ массив зовётся structures, а не trackside:
// половина перечня стоит не сбоку от пути, а под ним (разбор — в шапке типа
// Structure).
func (m *Map) validateStructures(elements map[string]bool) error {
	all := m.AllAlignments()
	seen := map[string]bool{}
	for _, st := range m.Topology.Structures {
		if err := checkEntity("сооружение", st.Name, st.ID); err != nil {
			return err
		}
		if seen[st.ID] {
			return fmt.Errorf("mapfmt: сооружение %q объявлено дважды", Labeled(st.Name, st.ID))
		}
		seen[st.ID] = true
		switch st.Kind {
		case "platform", "buffer_stop":
			// Стоит РЯДОМ с путём и на землю не влияет.
		case "bridge", "tunnel":
			// ИСКУССТВЕННОЕ СООРУЖЕНИЕ — узкий смысл слова внутри класса
			// structures: путь здесь НЕСОМ, а не обрамлён. Для симуляции сегодня
			// это аннотация; для рельефа — исключение: на протяжении моста и
			// тоннеля земля НЕ примиряется с осью пути (terrain.carriedSpans
			// отбирает ровно эти два вида). Без этого земляные работы сравняли бы
			// долину под мостом и прокопали траншею над тоннелем.
			//
			// Отсюда и правило на будущее: новый вид дописывается в верхнюю ветку,
			// если он землю не трогает, и в нижнюю, если несёт путь. Ветки
			// различаются не оформлением, а тем, кто их читает.
		default:
			return fmt.Errorf("mapfmt: сооружение %s: неизвестный kind %q", Labeled(st.Name, st.ID), st.Kind)
		}
		// Форма протяжённости — непустота, порядок концов, допустимость
		// направления — проверяется один раз на все слои (пакет netloc).
		if err := st.Span.Structural(); err != nil {
			return fmt.Errorf("mapfmt: сооружение %s: %w", Labeled(st.Name, st.ID), err)
		}
		for _, iv := range st.Span {
			if !elements[iv.Element] {
				return fmt.Errorf("mapfmt: сооружение %s ссылается на несуществующий элемент %s", Labeled(st.Name, st.ID), iv.Element)
			}
			u, err := horizontalLengthU(all[iv.Element])
			if err != nil {
				return fmt.Errorf("mapfmt: сооружение %s: длина элемента %s: %w", Labeled(st.Name, st.ID), iv.Element, err)
			}
			from, err := units.MetersToDistance(iv.From)
			if err != nil {
				return fmt.Errorf("mapfmt: сооружение %s: начало интервала: %w", Labeled(st.Name, st.ID), err)
			}
			to, err := units.MetersToDistance(iv.To)
			if err != nil {
				return fmt.Errorf("mapfmt: сооружение %s: конец интервала: %w", Labeled(st.Name, st.ID), err)
			}
			// Границы — в целых микрометрах, а не в метрах-float: округление к
			// ближайшему микрометру есть правило формата (спека §3), и
			// сравнение float'ов отвергало бы интервал, кончающийся ровно на
			// конце элемента. Порядок концов уже проверен выше.
			if from < 0 || to > u {
				return fmt.Errorf("mapfmt: сооружение %s: интервал [%v, %v] вне элемента %s длиной %s",
					Labeled(st.Name, st.ID), iv.From, iv.To, iv.Element, u)
			}
		}
		if st.Kind == "buffer_stop" {
			if err := m.checkBufferStopPort(st, all); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkBufferStopPort — упор обязан стоять там, где путь объявлен кончающимся.
//
// # Зачем проверка, если оба факта уже записаны
//
// Тупик выразим ДВАЖДЫ, и это надо было развести, а не оставить как есть
// (контракт отрисовки, редакция 6, §4.3):
//
//   - Port.Purpose == "buffer_stop" — ТОПОЛОГИЧЕСКОЕ утверждение: продолжения
//     нет и искать его не надо. Вход связности;
//   - Structure{Kind: "buffer_stop"} — ПОСТРОЕННОЕ сооружение с габаритом. Вход
//     отрисовки.
//
// Одно не влечёт другого: тупик может кончаться земляным валом или ничем, и
// тогда порт есть, а сооружения нет — это законно. Обратного не бывает: упор,
// стоящий там, где путь по топологии продолжается, есть расхождение двух записей
// об одном, а расхождение обязано быть отказом, а не выбором одной из версий.
//
// Проект уже лечил двойные имена одной вещи (ClearAhead-0jq, ClearAhead-8kx), и
// лечение записано: одно имя с оговоркой либо два с проверенной связью. Здесь
// выбрано второе, потому что смыслы разные — и вот проверка связи.
func (m *Map) checkBufferStopPort(st Structure, all map[string]Alignments) error {
	// Упор — точечное сооружение, и его span вырожден. Многоинтервальный упор
	// отвергается здесь, а не молча берётся первым интервалом: «упор на двух
	// концах» — это два упора с разными ID.
	if len(st.Span) != 1 {
		return fmt.Errorf("mapfmt: упор %s: ожидался один интервал, объявлено %d", st.ID, len(st.Span))
	}
	iv := st.Span[0]
	u, err := horizontalLengthU(all[iv.Element])
	if err != nil {
		return fmt.Errorf("mapfmt: упор %s: длина элемента %s: %w", st.ID, iv.Element, err)
	}
	from, _ := units.MetersToDistance(iv.From)
	to, _ := units.MetersToDistance(iv.To)
	if from != to {
		return fmt.Errorf("mapfmt: упор %s: интервал [%v, %v] не точечный", st.ID, iv.From, iv.To)
	}

	// Ребро ищется по ID элемента: упор на ПРОХОДЕ СТРЕЛКИ невозможен — проход
	// кончается портом стрелки, за которым продолжение есть по построению.
	var edge *Edge
	for i := range m.Topology.Edges {
		if m.Topology.Edges[i].ID == iv.Element {
			edge = &m.Topology.Edges[i]
			break
		}
	}
	if edge == nil {
		return fmt.Errorf("mapfmt: упор %s стоит на элементе %s, который не является ребром", st.ID, iv.Element)
	}

	// u растёт от порта From к порту To — соглашение распространения поз.
	port := ""
	switch {
	case from == 0:
		port = edge.From
	case from == u:
		port = edge.To
	default:
		return fmt.Errorf("mapfmt: упор %s стоит при u = %v внутри ребра %s длиной %s, а не на его конце",
			st.ID, iv.From, iv.Element, u)
	}

	ports, err := m.collectPorts()
	if err != nil {
		return err
	}
	p, ok := ports[port]
	if !ok {
		return fmt.Errorf("mapfmt: упор %s: ребро %s ссылается на несуществующий порт %s", st.ID, edge.ID, port)
	}
	if p.Purpose != "buffer_stop" {
		return fmt.Errorf(
			"mapfmt: упор %s стоит в порту %s с purpose %q: сооружение подтверждает тупик, а топология его не объявляет",
			st.ID, port, p.Purpose)
	}
	return nil
}

// horizontalLengthU — единственное место, где считается длина горизонтальной
// цепочки. И validateAlignments, и validateStructures зовут её: два независимых
// расчёта одного и того же числа рано или поздно разойдутся.
//
// Правило округления спеки §3: сумма индивидуально округлённых длин.
func horizontalLengthU(a Alignments) (units.Distance, error) {
	var u units.Distance
	for i, p := range a.Horizontal {
		var (
			d   units.Distance
			err error
		)
		switch p.Kind {
		case "straight":
			d, err = units.MetersToDistance(p.Length)
		case "arc":
			d, err = units.MetersToDistance(p.Radius * math.Abs(p.Angle))
		default:
			return 0, fmt.Errorf("неизвестный примитив плана %q на позиции %d", p.Kind, i)
		}
		if err != nil {
			return 0, fmt.Errorf("примитив %d: %w", i, err)
		}
		if d <= 0 {
			return 0, fmt.Errorf("примитив %d: вырожденная длина", i)
		}
		u += d
	}
	return u, nil
}

// validateAnchors проверяет, что якоря существуют и указывают на существующие
// порты.
//
// Требование якоря ОБУСЛОВЛЕНО наличием сети, а не безусловно: карта есть
// рецепт ПРИРОДЫ (спека world-layers-design §1) — путь живёт в партии, и по
// решению владельца «железная дорога никакого отношения к карте не имеет».
// Требование «хотя бы один якорь» было верным ровно до тех пор, пока путь жил
// в карте: якорь привязывал сеть к местности. После отвязки оно превратилось
// бы в утверждение «природы без дороги не бывает», а мир без единого метра
// пути обязан существовать. Поэтому карта без элементов пути законна БЕЗ
// якорей — но не С якорями: якорь в никуда есть ошибка автора, и молча его
// проигнорировать значило бы принять неверно написанную карту, поэтому отказ
// называет число объявленных якорей.
//
// «Ровно один якорь на связную компоненту» здесь НЕ проверяется: валидатор не
// строит компонент связности. Инвариант держит компилятор (track.Propagate),
// который отвергает и компоненту без якоря, и два якоря в одной. Комментарий
// первой редакции обещал проверку, которой не было, — исправлено на правду.
func (m *Map) validateAnchors(ports map[string]Port, elements map[string]bool) error {
	if len(elements) == 0 {
		if len(m.Anchors) > 0 {
			return fmt.Errorf("mapfmt: на карте нет ни одного элемента пути, но объявлено якорей: %d", len(m.Anchors))
		}
		return nil
	}
	if len(m.Anchors) == 0 {
		return fmt.Errorf("mapfmt: нет ни одного якоря")
	}
	for id := range m.Anchors {
		if _, ok := ports[id]; !ok {
			return fmt.Errorf("mapfmt: якорь ссылается на несуществующий порт %s", id)
		}
	}
	return nil
}
