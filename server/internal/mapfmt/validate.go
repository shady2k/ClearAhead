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

// Validate проверяет форму карты. Отказывает, не чинит.
func Validate(m *Map) error {
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

	for id, a := range m.AllAlignments() {
		if err := validateAlignments(id, a); err != nil {
			return err
		}
	}
	if err := m.validateEdgeEnds(ports); err != nil {
		return err
	}
	if err := m.validateTrackside(elements); err != nil {
		return err
	}
	if err := validateGeoreference(m.Georeference); err != nil {
		return err
	}
	if err := m.validateAnchors(ports); err != nil {
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
	return m.validateProfile(DefaultProfile())
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
	// Длина считается единственной функцией — той же, что зовёт validateTrackside.
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
		if err := ValidID("узел", n.ID); err != nil {
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
		if t.Hand != "left" && t.Hand != "right" {
			return nil, fmt.Errorf("mapfmt: стрелка %s: рукость должна быть left или right, получено %q", t.ID, t.Hand)
		}
		// Марка крестовины НЕ проверяется намеренно: §8 объявляет её
		// происхождением, а не ограничением, ради импорта реальных станций с
		// произвольными радиусами. Клиент строит крестовину из геометрии, а не
		// из марки.
		for _, p := range []string{t.Ports.Common, t.Ports.Straight, t.Ports.Diverging} {
			if p == "" {
				return nil, fmt.Errorf("mapfmt: стрелка %s: не заняты все три порта", t.ID)
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
		if err := ValidID("ребро", e.ID); err != nil {
			return nil, err
		}
		if err := validKind("ребро", e.ID, e.Kind); err != nil {
			return nil, err
		}
		if els[e.ID] {
			return nil, fmt.Errorf("mapfmt: ребро %q объявлено дважды", e.ID)
		}
		els[e.ID] = true
		if _, ok := m.Geometry.Edges[e.ID]; !ok {
			return nil, fmt.Errorf("mapfmt: у ребра %s нет геометрии", e.ID)
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
		if err := ValidID("стрелка", t.ID); err != nil {
			return nil, err
		}
		// Вид устройства проверяется наравне с видом ребра: проходы стрелки —
		// такие же адресуемые элементы сети, и берут они его отсюда.
		if err := validKind("стрелка", t.ID, t.Kind); err != nil {
			return nil, err
		}
		if devs[t.ID] {
			return nil, fmt.Errorf("mapfmt: стрелка %q объявлена дважды", t.ID)
		}
		devs[t.ID] = true
		if _, ok := m.Geometry.Turnouts[t.ID]; !ok {
			return nil, fmt.Errorf("mapfmt: у стрелки %s нет геометрии", t.ID)
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

func (m *Map) validateTrackside(elements map[string]bool) error {
	all := m.AllAlignments()
	seen := map[string]bool{}
	for _, ts := range m.Topology.Trackside {
		if err := ValidID("путевой объект", ts.ID); err != nil {
			return err
		}
		if seen[ts.ID] {
			return fmt.Errorf("mapfmt: путевой объект %q объявлен дважды", ts.ID)
		}
		seen[ts.ID] = true
		switch ts.Kind {
		case "platform", "buffer_stop":
		case "bridge", "tunnel":
			// Искусственное сооружение. Для симуляции сегодня это аннотация;
			// для рельефа — исключение: на протяжении моста и тоннеля земля НЕ
			// примиряется с осью пути (см. пакет terrain). Без этого земляные
			// работы сравняли бы долину под мостом и прокопали траншею над
			// тоннелем.
		default:
			return fmt.Errorf("mapfmt: путевой объект %s: неизвестный kind %q", ts.ID, ts.Kind)
		}
		// Форма протяжённости — непустота, порядок концов, допустимость
		// направления — проверяется один раз на все слои (пакет netloc).
		if err := ts.Span.Structural(); err != nil {
			return fmt.Errorf("mapfmt: путевой объект %s: %w", ts.ID, err)
		}
		for _, iv := range ts.Span {
			if !elements[iv.Element] {
				return fmt.Errorf("mapfmt: путевой объект %s ссылается на несуществующий элемент %s", ts.ID, iv.Element)
			}
			u, err := horizontalLengthU(all[iv.Element])
			if err != nil {
				return fmt.Errorf("mapfmt: путевой объект %s: длина элемента %s: %w", ts.ID, iv.Element, err)
			}
			from, err := units.MetersToDistance(iv.From)
			if err != nil {
				return fmt.Errorf("mapfmt: путевой объект %s: начало интервала: %w", ts.ID, err)
			}
			to, err := units.MetersToDistance(iv.To)
			if err != nil {
				return fmt.Errorf("mapfmt: путевой объект %s: конец интервала: %w", ts.ID, err)
			}
			// Границы — в целых микрометрах, а не в метрах-float: округление к
			// ближайшему микрометру есть правило формата (спека §3), и
			// сравнение float'ов отвергало бы интервал, кончающийся ровно на
			// конце элемента. Порядок концов уже проверен выше.
			if from < 0 || to > u {
				return fmt.Errorf("mapfmt: путевой объект %s: интервал [%v, %v] вне элемента %s длиной %s",
					ts.ID, iv.From, iv.To, iv.Element, u)
			}
		}
	}
	return nil
}

// horizontalLengthU — единственное место, где считается длина горизонтальной
// цепочки. И validateAlignments, и validateTrackside зовут её: два независимых
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
// «Ровно один якорь на связную компоненту» здесь НЕ проверяется: валидатор не
// строит компонент связности. Инвариант держит компилятор (track.Propagate),
// который отвергает и компоненту без якоря, и два якоря в одной. Комментарий
// первой редакции обещал проверку, которой не было, — исправлено на правду.
func (m *Map) validateAnchors(ports map[string]Port) error {
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
