package edit

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// applyIntent применяет правку к копии карты и валидирует результат.
// Общий конвейер для Preview и Apply: предпросмотр и факт не могут разойтись.
//
// Run'ы решётки обновляются внутри операций хирургически: решётка —
// авторитетный факт о физике, а не производная от нарезки на элементы
// (требование 6), см. runs.go.
func applyIntent(cur *mapfmt.Map, maxRev int, i Intent) (Result, error) {
	m := cloneMap(cur)
	var prev *ErasePreview
	var err error
	switch i.Op {
	case OpExtend:
		err = applyExtend(&m, i.Extend)
	case OpBranch:
		err = applyBranch(&m, i.Branch)
	case OpCap:
		err = applyCap(&m, i.Cap)
	case OpPlace:
		err = applyPlace(&m, i.Place)
	case OpErase:
		prev, err = applyErase(&m, i.Erase)
	default:
		return Result{}, fmt.Errorf("edit: неизвестная правка %d", i.Op)
	}
	if err != nil {
		return Result{}, err
	}
	if err := mapfmt.Validate(&m); err != nil {
		return Result{}, fmt.Errorf("edit: результат правки не проходит валидацию: %w", err)
	}
	m.MapRevision = maxRev + 1
	return Result{Map: m, Revision: m.MapRevision, Cascade: prev}, nil
}

// ---- Общие помощники ----

// portEndCount — число концов рёбер в порту.
func portEndCount(m *mapfmt.Map, port string) int {
	n := 0
	for _, e := range m.Topology.Edges {
		if e.From == port || e.To == port {
			n++
		}
	}
	return n
}

// isTurnoutPort — порт принадлежит стрелке.
func isTurnoutPort(m *mapfmt.Map, port string) bool {
	for _, t := range m.Topology.Turnouts {
		for _, p := range []string{t.Ports.Common, t.Ports.Straight, t.Ports.Diverging} {
			if port == t.ID+"."+p {
				return true
			}
		}
	}
	return false
}

// turnoutPorts возвращает полные ID трёх портов стрелки.
func turnoutPorts(t *mapfmt.Turnout) []string {
	return []string{t.ID + "." + t.Ports.Common, t.ID + "." + t.Ports.Straight, t.ID + "." + t.Ports.Diverging}
}

// allocID — свободный ID из общего пространства имён карты: prefix, затем
// prefix2, prefix3, … Коллизий с существующими элементами не бывает.
func allocID(m *mapfmt.Map, prefix string) string {
	used := map[string]bool{}
	for _, e := range m.Topology.Edges {
		used[e.ID] = true
	}
	for _, t := range m.Topology.Turnouts {
		used[t.ID] = true
	}
	for _, n := range m.Topology.Nodes {
		used[n.ID] = true
	}
	for _, ts := range m.Topology.Trackside {
		used[ts.ID] = true
	}
	if !used[prefix] {
		return prefix
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s%d", prefix, i)
		if !used[cand] {
			return cand
		}
	}
}

// chainToAlignments переводит цепочку примитивов решателя в выравнивания
// карты. Вертикальный профиль не создаётся: новые элементы плоские.
func chainToAlignments(c geom.Chain) (mapfmt.Alignments, error) {
	if len(c) == 0 {
		return mapfmt.Alignments{}, fmt.Errorf("пустая цепочка")
	}
	h := make([]mapfmt.HPrim, 0, len(c))
	for i, p := range c {
		switch p.Kind {
		case geom.KindStraight:
			if p.Length <= 0 {
				return mapfmt.Alignments{}, fmt.Errorf("примитив %d: неположительная длина", i)
			}
			h = append(h, mapfmt.HPrim{Kind: "straight", Length: p.Length.Meters()})
		case geom.KindArc:
			h = append(h, mapfmt.HPrim{Kind: "arc", Radius: p.Radius, Angle: p.Angle})
		default:
			return mapfmt.Alignments{}, fmt.Errorf("примитив %d: неизвестный вид %d", i, p.Kind)
		}
	}
	return mapfmt.Alignments{Horizontal: h}, nil
}

// alignmentsLengthU — длина горизонтальной цепочки в целых микрометрах с
// правилом округления спеки §3 (сумма индивидуально округлённых длин).
// Зеркалит mapfmt.horizontalLengthU, недоступную снаружи; валидатор — чужая
// собственность, менять его нельзя, поэтому правило повторено здесь и
// покрыто тестом на совпадение с валидатором.
func alignmentsLengthU(a mapfmt.Alignments) (units.Distance, error) {
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
			return 0, fmt.Errorf("примитив %d: неизвестный вид %q", i, p.Kind)
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

// checkEndPurpose — разрешённое назначение нового открытого конца.
func checkEndPurpose(purpose string) error {
	switch purpose {
	case "", "buffer_stop", "map_boundary":
		return nil
	}
	return fmt.Errorf("неизвестное назначение конца %q, ожидается buffer_stop или map_boundary", purpose)
}

// ---- Продлить путь от порта ----

func applyExtend(m *mapfmt.Map, in ExtendIntent) error {
	if in.Port == "" {
		return fmt.Errorf("edit: продление: не указан порт")
	}
	if isTurnoutPort(m, in.Port) {
		return fmt.Errorf("edit: продление: порт %s принадлежит стрелке — валидатор требует ровно одно внешнее ребро на порт стрелки", in.Port)
	}
	if portEndCount(m, in.Port) != 1 {
		return fmt.Errorf("edit: продление: порт %s не лист (ровно одно ребро), концов %d", in.Port, portEndCount(m, in.Port))
	}
	// Ребро обязано заканчиваться в порту, иначе продолжение развернулось бы
	// вдоль приходящего пути и легло поверх него — валидатор отверг бы
	// пересечение осей, но внятнее отказать заранее.
	leafEndsHere := false
	for _, e := range m.Topology.Edges {
		if e.To == in.Port {
			leafEndsHere = true
			break
		}
	}
	if !leafEndsHere {
		return fmt.Errorf("edit: продление: ребро у порта %s начинается в нём, а не заканчивается — продолжение легло бы на существующий путь", in.Port)
	}
	if len(in.Chain) == 0 {
		return fmt.Errorf("edit: продление: пустая цепочка")
	}
	if err := checkEndPurpose(in.EndPurpose); err != nil {
		return fmt.Errorf("edit: продление: %w", err)
	}
	al, err := chainToAlignments(in.Chain)
	if err != nil {
		return fmt.Errorf("edit: продление: %w", err)
	}
	purpose := in.EndPurpose
	if purpose == "" {
		purpose = "buffer_stop"
	}

	edgeID := allocID(m, "E_EXT")
	nodeID := allocID(m, "N_EXT")
	m.Topology.Edges = append(m.Topology.Edges, mapfmt.Edge{ID: edgeID, From: in.Port, To: nodeID + ".P1"})
	m.Topology.Nodes = append(m.Topology.Nodes, mapfmt.Node{
		ID:    nodeID,
		Ports: []mapfmt.Port{{ID: "P1", Purpose: purpose}},
	})
	if m.Geometry.Edges == nil {
		m.Geometry.Edges = map[string]mapfmt.Alignments{}
	}
	m.Geometry.Edges[edgeID] = al

	// Решётка: новое ребро вливается в run ребра, заканчивавшегося в порту —
	// шпалы продолжаются через стык как были.
	if m.Construction != nil {
		if m.Construction.Runs, err = extendRuns(m, m.Construction.Runs, in.Port, edgeID); err != nil {
			return fmt.Errorf("edit: продление: run'ы: %w", err)
		}
	}
	return nil
}

// ---- Ответвиться от существующего элемента ----

func applyBranch(m *mapfmt.Map, in BranchIntent) error {
	idx := -1
	for i := range m.Topology.Edges {
		if m.Topology.Edges[i].ID == in.Edge {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("edit: ветвление: ребро %s не найдено", in.Edge)
	}
	edge := m.Topology.Edges[idx]
	al, ok := m.Geometry.Edges[edge.ID]
	if !ok {
		return fmt.Errorf("edit: ветвление: у ребра %s нет геометрии", edge.ID)
	}
	if len(al.Vertical) > 0 {
		return fmt.Errorf("edit: ветвление: ребро %s с вертикальным профилем — рез по вертикали не реализован, эскалируй", edge.ID)
	}
	u, err := alignmentsLengthU(al)
	if err != nil {
		return fmt.Errorf("edit: ветвление: длина ребра %s: %w", edge.ID, err)
	}
	if !(in.AtU > 0 && in.AtU < u.Meters()) {
		return fmt.Errorf("edit: ветвление: точка %v вне (0, %v) ребра %s", in.AtU, u, edge.ID)
	}
	if in.Hand != "left" && in.Hand != "right" {
		return fmt.Errorf("edit: ветвление: рукость %q, ожидается left или right", in.Hand)
	}
	if len(in.Straight) == 0 || len(in.Diverging) == 0 || len(in.Branch) == 0 {
		return fmt.Errorf("edit: ветвление: нужны геометрии прямого и отклонённого проходов и ветви")
	}
	if err := checkEndPurpose(in.EndPurpose); err != nil {
		return fmt.Errorf("edit: ветвление: %w", err)
	}
	straightAL, err := chainToAlignments(in.Straight)
	if err != nil {
		return fmt.Errorf("edit: ветвление: прямой проход: %w", err)
	}
	divergingAL, err := chainToAlignments(in.Diverging)
	if err != nil {
		return fmt.Errorf("edit: ветвление: отклонённый проход: %w", err)
	}
	branchAL, err := chainToAlignments(in.Branch)
	if err != nil {
		return fmt.Errorf("edit: ветвление: ветвь: %w", err)
	}
	purpose := in.EndPurpose
	if purpose == "" {
		purpose = "buffer_stop"
	}

	head, tail, err := splitAlignments(al.Horizontal, in.AtU)
	if err != nil {
		return fmt.Errorf("edit: ветвление: рез ребра %s: %w", edge.ID, err)
	}

	swID := allocID(m, "SW")
	contID := allocID(m, edge.ID+"@2")
	branchID := allocID(m, swID+"_BR")
	endNode := allocID(m, "N_"+swID+"_BR")

	// Стрелка и её геометрия.
	m.Topology.Turnouts = append(m.Topology.Turnouts, mapfmt.Turnout{
		ID:   swID,
		Hand: in.Hand,
		Ports: mapfmt.TurnoutPorts{
			Common:    "C",
			Straight:  "S",
			Diverging: "D",
		},
	})
	if m.Geometry.Turnouts == nil {
		m.Geometry.Turnouts = map[string]mapfmt.TurnoutGeometry{}
	}
	m.Geometry.Turnouts[swID] = mapfmt.TurnoutGeometry{Straight: straightAL, Diverging: divergingAL}

	// Подходная часть сохраняет ID исходного ребра (фаза run'а наследуется),
	// продолжение и ветвь — новые рёбра.
	m.Topology.Edges[idx].To = swID + ".C"
	m.Geometry.Edges[edge.ID] = mapfmt.Alignments{Horizontal: head}

	m.Topology.Edges = append(m.Topology.Edges, mapfmt.Edge{ID: contID, From: swID + ".S", To: edge.To})
	m.Geometry.Edges[contID] = mapfmt.Alignments{Horizontal: tail}

	m.Topology.Edges = append(m.Topology.Edges, mapfmt.Edge{ID: branchID, From: swID + ".D", To: endNode + ".P1"})
	m.Topology.Nodes = append(m.Topology.Nodes, mapfmt.Node{
		ID:    endNode,
		Ports: []mapfmt.Port{{ID: "P1", Purpose: purpose}},
	})
	m.Geometry.Edges[branchID] = branchAL

	// Решётка: спад разрезанного ребра делится надвое в том же направлении
	// (шпалы не переставляются), ветвь получает собственный run.
	if m.Construction != nil {
		if m.Construction.Runs, err = splitRuns(m, m.Construction.Runs, edge.ID, contID); err != nil {
			return fmt.Errorf("edit: ветвление: run'ы: %w", err)
		}
		brRun, err := newRunForEdge(m, branchID)
		if err != nil {
			return fmt.Errorf("edit: ветвление: run ветви: %w", err)
		}
		m.Construction.Runs = append(m.Construction.Runs, brRun)
	}
	return nil
}

// splitAlignments режет горизонтальную цепочку в точке u=c. Примитив, внутри
// которого лежит рез, делится на два (дуга — пропорционально углу); рез ровно
// на стыке оставляет целые части. Обе части обязаны дать ненулевую длину.
func splitAlignments(h []mapfmt.HPrim, c float64) (head, tail []mapfmt.HPrim, err error) {
	primLen := func(p mapfmt.HPrim) (float64, error) {
		switch p.Kind {
		case "straight":
			return p.Length, nil
		case "arc":
			return p.Radius * math.Abs(p.Angle), nil
		}
		return 0, fmt.Errorf("неизвестный примитив плана %q", p.Kind)
	}

	var acc float64
	cut := -1
	var part float64
	for i, p := range h {
		plen, err := primLen(p)
		if err != nil {
			return nil, nil, err
		}
		switch {
		case c >= acc+plen: // рез после примитива — целиком в голову
			head = append(head, p)
		case c <= acc: // рез до примитива — целиком в хвост
			tail = append(tail, p)
		default: // внутри примитива
			cut = i
			part = c - acc
		}
		acc += plen
	}
	if cut < 0 {
		return nil, nil, fmt.Errorf("точка %v вне (0, %v)", c, acc)
	}
	p := h[cut]
	switch p.Kind {
	case "straight":
		head = append(head, mapfmt.HPrim{Kind: "straight", Length: part})
		tail = append([]mapfmt.HPrim{{Kind: "straight", Length: p.Length - part}}, tail...)
	case "arc":
		frac := part / (p.Radius * math.Abs(p.Angle))
		head = append(head, mapfmt.HPrim{Kind: "arc", Radius: p.Radius, Angle: p.Angle * frac})
		tail = append([]mapfmt.HPrim{{Kind: "arc", Radius: p.Radius, Angle: p.Angle * (1 - frac)}}, tail...)
	}

	// Части обязаны округлиться в ненулевые длины, иначе валидатор отвергнет
	// вырожденный примитив, а внятнее назвать причину здесь.
	for i, pr := range append(append([]mapfmt.HPrim{}, head...), tail...) {
		plen, err := primLen(pr)
		if err != nil {
			return nil, nil, err
		}
		d, err := units.MetersToDistance(plen)
		if err != nil || d <= 0 {
			return nil, nil, fmt.Errorf("рез слишком близко к стыку: часть %d вырождается", i)
		}
	}
	return head, tail, nil
}

// ---- Замкнуть конец тупиковым упором ----

func applyCap(m *mapfmt.Map, in CapIntent) error {
	if in.Port == "" {
		return fmt.Errorf("edit: упор: не указан порт")
	}
	for i := range m.Topology.Nodes {
		for j := range m.Topology.Nodes[i].Ports {
			if m.Topology.Nodes[i].ID+"."+m.Topology.Nodes[i].Ports[j].ID == in.Port {
				m.Topology.Nodes[i].Ports[j].Purpose = "buffer_stop"
				return nil
			}
		}
	}
	if isTurnoutPort(m, in.Port) {
		return fmt.Errorf("edit: упор: порт %s принадлежит стрелке — формат не несёт purpose на портах стрелки", in.Port)
	}
	return fmt.Errorf("edit: упор: порт %s не найден", in.Port)
}

// ---- Положить платформу на участок ----

func applyPlace(m *mapfmt.Map, in PlaceIntent) error {
	al, ok := m.Alignments(in.Element)
	if !ok {
		return fmt.Errorf("edit: платформа: элемент %s не найден", in.Element)
	}
	u, err := alignmentsLengthU(al)
	if err != nil {
		return fmt.Errorf("edit: платформа: длина элемента %s: %w", in.Element, err)
	}
	if !(in.From >= 0 && in.To > in.From) {
		return fmt.Errorf("edit: платформа: интервал [%v, %v] вырожден", in.From, in.To)
	}
	if in.To > u.Meters() {
		return fmt.Errorf("edit: платформа: конец %v за пределами элемента %s (длина %s)", in.To, in.Element, u)
	}
	if in.Side == "" {
		in.Side = "right"
	}
	if in.Side != "left" && in.Side != "right" {
		return fmt.Errorf("edit: платформа: сторона %q, ожидается left или right", in.Side)
	}
	// Границы валидатора — единственный источник правды о размерах (спека §3);
	// здесь проверяем заранее ради внятной ошибки, вердикт всё равно за ним.
	if !(in.Offset >= 1.0 && in.Offset <= 4.0) {
		return fmt.Errorf("edit: платформа: offset %v вне [1, 4]", in.Offset)
	}
	if !(in.Width >= 1.0 && in.Width <= 12.0) {
		return fmt.Errorf("edit: платформа: width %v вне [1, 12]", in.Width)
	}

	m.Topology.Trackside = append(m.Topology.Trackside, mapfmt.Trackside{
		ID:     allocID(m, "PLAT"),
		Kind:   "platform",
		Span:   []mapfmt.SpanInterval{{Element: in.Element, From: in.From, To: in.To}},
		Side:   in.Side,
		Offset: in.Offset,
		Width:  in.Width,
	})
	return nil
}

// ---- Стереть ----

func applyErase(m *mapfmt.Map, in EraseIntent) (*ErasePreview, error) {
	if in.Target == "" {
		return nil, fmt.Errorf("edit: стирка: не указана цель")
	}
	targetIsEdge := false
	for _, e := range m.Topology.Edges {
		if e.ID == in.Target {
			targetIsEdge = true
			break
		}
	}
	targetIsTurnout := false
	for _, t := range m.Topology.Turnouts {
		if t.ID == in.Target {
			targetIsTurnout = true
			break
		}
	}
	if !targetIsEdge && !targetIsTurnout {
		return nil, fmt.Errorf("edit: стирка: цель %s не ребро и не стрелка", in.Target)
	}

	// Каскад — фиксированная точка: стрелка уходит, если любое её внешнее
	// ребро ушло (порт стрелки без ребра валидатор отвергает); ребро уходит,
	// если оно инцидентно ушедшей стрелке или это цель.
	removed := map[string]bool{in.Target: true}
	for changed := true; changed; {
		changed = false
		for i := range m.Topology.Turnouts {
			t := m.Topology.Turnouts[i]
			if removed[t.ID] {
				continue
			}
			for _, p := range turnoutPorts(&t) {
				if portHasRemovedEdge(m, p, removed) {
					removed[t.ID] = true
					changed = true
					break
				}
			}
		}
		for _, e := range m.Topology.Edges {
			if removed[e.ID] {
				continue
			}
			for i := range m.Topology.Turnouts {
				t := m.Topology.Turnouts[i]
				if !removed[t.ID] {
					continue
				}
				for _, p := range turnoutPorts(&t) {
					if e.From == p || e.To == p {
						removed[e.ID] = true
						changed = true
					}
				}
			}
		}
	}

	if in.Mode == EraseSelection && len(removed) > 1 {
		extra := make([]string, 0, len(removed)-1)
		for id := range removed {
			if id != in.Target {
				extra = append(extra, id)
			}
		}
		sort.Strings(extra)
		return nil, fmt.Errorf("edit: стирка выбора: каскад уносит сверх выбранного: %s — используй режим каскада", strings.Join(extra, ", "))
	}

	prev := &ErasePreview{
		RemovedElements: sortedSet(removed),
	}

	// Применяем каскад. Узлы и их порты сохраняются: порт с назначением и без
	// рёбер законен, а удаление узлов потребовало бы управлять якорями.
	edges := m.Topology.Edges[:0]
	for _, e := range m.Topology.Edges {
		if !removed[e.ID] {
			edges = append(edges, e)
		}
	}
	m.Topology.Edges = edges

	turnouts := m.Topology.Turnouts[:0]
	for _, t := range m.Topology.Turnouts {
		if removed[t.ID] {
			delete(m.Geometry.Turnouts, t.ID)
			continue
		}
		turnouts = append(turnouts, t)
	}
	m.Topology.Turnouts = turnouts

	for id := range m.Geometry.Edges {
		if removed[id] {
			delete(m.Geometry.Edges, id)
		}
	}

	// Путевые объекты, чьи спаны лежат на удалённых элементах (включая
	// проходы удалённых стрелок), рвутся и уходят целиком.
	trackside := m.Topology.Trackside[:0]
	for _, ts := range m.Topology.Trackside {
		broken := false
		for _, sp := range ts.Span {
			if removed[sp.Element] {
				broken = true
				break
			}
			if sw, ok := passageTurnout(sp.Element); ok && removed[sw] {
				broken = true
				break
			}
		}
		if broken {
			prev.RemovedTrackside = append(prev.RemovedTrackside, ts.ID)
			continue
		}
		trackside = append(trackside, ts)
	}
	m.Topology.Trackside = trackside
	sort.Strings(prev.RemovedTrackside)

	// Висящие концы: порт узла с менее чем двумя рёбрами и без назначения
	// закрывается упором — валидатор отвергает висящее ребро без purpose.
	// Список уходит в предпросмотр: игрок видит, что станет упором.
	for i := range m.Topology.Nodes {
		for j := range m.Topology.Nodes[i].Ports {
			p := &m.Topology.Nodes[i].Ports[j]
			port := m.Topology.Nodes[i].ID + "." + p.ID
			if portEndCount(m, port) < 2 && p.Purpose == "" {
				p.Purpose = "buffer_stop"
				prev.CappedPorts = append(prev.CappedPorts, port)
			}
		}
	}
	sort.Strings(prev.CappedPorts)

	// Решётка: спаны удалённых рёбер уходят, пустые run'ы исчезают.
	if m.Construction != nil {
		m.Construction.Runs = dropRuns(m.Construction.Runs, removed)
	}
	return prev, nil
}

// portHasRemovedEdge — у порта есть инцидентное ребро из множества removed.
func portHasRemovedEdge(m *mapfmt.Map, port string, removed map[string]bool) bool {
	for _, e := range m.Topology.Edges {
		if removed[e.ID] && (e.From == port || e.To == port) {
			return true
		}
	}
	return false
}

// passageTurnout возвращает ID стрелки, если элемент — проход стрелки.
func passageTurnout(element string) (string, bool) {
	for _, suffix := range []string{mapfmt.PassageStraight, mapfmt.PassageDiverging} {
		if strings.HasSuffix(element, suffix) {
			return strings.TrimSuffix(element, suffix), true
		}
	}
	return "", false
}

func sortedSet(s map[string]bool) []string {
	out := make([]string, 0, len(s))
	for id := range s {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
