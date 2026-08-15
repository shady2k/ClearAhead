package mapfmt

import (
	"strings"
	"testing"
)

// Достижим ли был дефект «две стрелки с одним ID»?
//
// Утверждалось: без проверки повтора две стрелки с одним ID молча схлопываются
// в одну запись скомпилированной топологии. Первая проба этого НЕ показала —
// карта отвергалась как «порт стрелки не соединён ребром», то есть недоделанной
// была проба, а не дефект недостижимым: порты стрелки просто не заняли рёбрами.
// Здесь проба доведена до конца.
//
// РЕЗУЛЬТАТ: схлопывание реально (TestDuplicateTurnoutCollapsesPassages — оба
// прохода достаются ЛИШЬ ОДНОЙ из двух стрелок), но карта с таким дубликатом
// невалидна и БЕЗ проверки повтора: её ловит правило «сколько проходов у порта
// стрелки» в разборе стыков (validateAxisIntersections). Проверка повтора —
// страховка, называющая причину прямо, а не починка достижимой дыры.
//
// Почему обойти нельзя:
//
//   - collectPorts требует уникальности КВАЛИФИЦИРОВАННОГО имени порта, значит
//     наборы имён портов двух одноимённых стрелок обязаны не пересекаться:
//     SW.C/SW.S/SW.D у одной и SW.C2/SW.S2/SW.D2 у другой;
//   - Geometry.Turnouts — map по ID стрелки, значит геометрия у них ОДНА, и
//     проходов в карте ровно два: SW:straight и SW:diverging;
//   - PassageEnds — тоже map по ID прохода, значит концы этих двух проходов
//     достаются одной стрелке (последней в списке). Три порта другой остаются
//     без единого прохода;
//   - validateEdgeEnds требует, чтобы каждый порт стрелки был занят ребром.
//     Это выполнимо — ставятся шесть рёбер;
//   - но тогда все шесть портов попадают в разбор стыков, а он требует у порта
//     стрелки ровно два прохода (у общего) или ровно один (у остальных).
//     У трёх портов обделённой стрелки проходов ноль.
//
// Выбора без третьего варианта: не занять порты рёбрами — отказ
// validateEdgeEnds; занять — отказ по числу проходов у порта стрелки.

// mapWithTwoTurnouts строит карту, в которой ВСЕ инварианты удовлетворены
// нарочно: порты стрелок различны и каждый занят своим ребром, у каждого ребра
// есть геометрия, у стрелки есть геометрия по ключу ID, висящие концы объявлены
// упорами, якорь один и стоит на порту с одним концом, оси не пересекаются.
//
// Единственная переменная — идентификаторы стрелок (UUID из зеркала таблицы
// tID: uIDSW1/uIDSW2, метки SW1/SW2). Одинаковые дают дефект, разные —
// контрольную карту, которая обязана проходить валидацию целиком. Узлы и рёбра
// несут свои UUID и метки: тождество элемента — UUID, читаемая метка — name.
func mapWithTwoTurnouts(id1, id2 string) *Map {
	straight := func(l float64) Alignments {
		return Alignments{Horizontal: []HPrim{{Kind: "straight", Length: l}}}
	}
	// Боковой проход обязан РАСХОДИТЬСЯ с прямым: два коллинеарных прохода из
	// одного острия — наложение осей, и карту отвергли бы за него.
	diverging := Alignments{Horizontal: []HPrim{{Kind: "arc", Radius: 200, Angle: 0.15}}}

	// Порты второй стрелки названы иначе даже при одинаковом ID: одинаковые
	// имена дали бы повтор квалифицированного порта, и карта умерла бы раньше.
	turnouts := []Turnout{
		{ID: id1, Name: "SW1", Kind: KindRail, Hand: "right", Drive: DriveManual, Ports: TurnoutPorts{Common: "C", Straight: "S", Diverging: "D"}},
		{ID: id2, Name: "SW2", Kind: KindRail, Hand: "left", Drive: DriveElectric, Ports: TurnoutPorts{Common: "C2", Straight: "S2", Diverging: "D2"}},
	}

	// Идентификаторы шести рёбер — по одному на порт узла PA..PF, метки EA..EF.
	edgeIDs := []string{uIDEA, uIDEB, uIDEC, uIDED, uIDEE, uIDEF}
	ports := []Port{}
	edges := []Edge{}
	edgeGeometry := map[string]Alignments{}
	turnoutGeometry := map[string]TurnoutGeometry{}
	for _, t := range turnouts {
		turnoutGeometry[t.ID] = TurnoutGeometry{Straight: straight(30), Diverging: diverging}
	}
	i := 0
	for _, t := range turnouts {
		for _, end := range t.PortIDs() {
			name := string(rune('A' + i))
			id := edgeIDs[i]
			i++
			ports = append(ports, Port{ID: "P" + name, Purpose: "buffer_stop"})
			edges = append(edges, Edge{ID: id, Name: "E" + name, Kind: KindRail, From: uIDN1 + ".P" + name, To: end})
			edgeGeometry[id] = straight(100)
		}
	}
	return &Map{
		FormatVersion: FormatVersion,
		MapID:         "ST_A",
		MapRevision:   1,
		Anchors:       map[string]Anchor{uIDN1 + ".PA": {}},
		Topology: Topology{
			Nodes:    []Node{{ID: uIDN1, Name: "N1", Ports: ports}},
			Turnouts: turnouts,
			Edges:    edges,
		},
		Geometry: Geometry{Turnouts: turnoutGeometry, Edges: edgeGeometry},
	}
}

// Контроль: та же карта с РАЗНЫМИ идентификаторами стрелок проходит валидацию
// целиком. Без него разбор ниже ничего не стоил бы: карта, сломанная ещё
// чем-нибудь, отвергалась бы по любой причине, и вывод о повторе был бы
// подгонкой.
func TestControlMapWithoutDuplicateIsValid(t *testing.T) {
	if err := Validate(mapWithTwoTurnouts(uIDSW1, uIDSW2)); err != nil {
		t.Fatalf("контрольная карта отвергнута: %v — тогда разбор повтора недоказателен", err)
	}
}

// Дубликат ID стрелки отвергается прямо — с указанием причины, а не окольным
// правилом про число проходов у порта. Текст называет метку второй стрелки
// рядом с UUID (Labeled): по одному UUID автор дефект не найдёт.
func TestDuplicateTurnoutIsRejected(t *testing.T) {
	err := Validate(mapWithTwoTurnouts(uIDSW1, uIDSW1))
	if err == nil {
		t.Fatal("карта с двумя стрелками одного ID принята")
	}
	if !strings.Contains(err.Error(), `стрелка "`+Labeled("SW2", uIDSW1)+`" объявлена дважды`) {
		t.Fatalf("отказ пришёл не по той причине: %v", err)
	}
}

// Схлопывание, о котором шла речь, действительно происходит: концы проходов
// живут в карте по ID прохода, поэтому вторая стрелка ЗАТИРАЕТ первую, и оба
// прохода целиком достаются одной из двух.
//
// Тест закрепляет сам механизм — он и есть причина, по которой повтор ID
// недопустим, независимо от того, каким правилом карта отвергается сегодня.
func TestDuplicateTurnoutCollapsesPassages(t *testing.T) {
	m := mapWithTwoTurnouts(uIDSW1, uIDSW1)

	if len(m.Geometry.Turnouts) != 1 {
		t.Fatalf("геометрия стрелок: записей %d, у двух одноимённых стрелок она одна", len(m.Geometry.Turnouts))
	}
	ends := m.PassageEnds()
	if len(ends) != 2 {
		t.Fatalf("проходов %d, ожидалось 2: два имени на две стрелки", len(ends))
	}
	// Последняя в списке стрелка выиграла: её порты стоят концами обоих
	// проходов, портов первой в концах нет вовсе.
	if got := ends[uIDSW1+PassageStraight]; got != [2]string{uIDSW1 + ".C2", uIDSW1 + ".S2"} {
		t.Fatalf("концы прямого прохода: %v", got)
	}
	if got := ends[uIDSW1+PassageDiverging]; got != [2]string{uIDSW1 + ".C2", uIDSW1 + ".D2"} {
		t.Fatalf("концы бокового прохода: %v", got)
	}
}

// Дефект был НЕДОСТИЖИМ: карта отвергается и без проверки повтора.
//
// Проверка повтора живёт в collectElements, поэтому прежнее поведение
// воспроизводится вызовом остальных модулей напрямую, а не правкой валидатора.
// Порядок тот же, что в Validate.
func TestDuplicateTurnoutWasCaughtWithoutDuplicateCheck(t *testing.T) {
	m := mapWithTwoTurnouts(uIDSW1, uIDSW1)

	// Порты: шесть квалифицированных имён стрелок различны, повтора нет.
	ports, err := m.collectPorts()
	if err != nil {
		t.Fatalf("сбор портов отверг карту: %v — тогда дубликат ловился бы уже здесь", err)
	}
	if len(ports) != 12 {
		t.Fatalf("портов %d, ожидалось 12 (6 узловых + 6 стрелочных)", len(ports))
	}

	// Концы рёбер: каждый порт стрелки занят ребром. Именно это правило
	// отвергало недоделанную пробу, и оно удовлетворимо без труда.
	if err := m.validateEdgeEnds(ports); err != nil {
		t.Fatalf("концы рёбер отвергли карту: %v — а это правило удовлетворимо", err)
	}

	// Выравнивания и якоря тоже проходят.
	for id, a := range m.AllAlignments() {
		if err := validateAlignments(id, a); err != nil {
			t.Fatalf("выравнивания %s отвергнуты: %v", id, err)
		}
	}
	// Элементы собираются мимо collectElements намеренно: карта несёт две
	// стрелки с одним ID, и сбор элементов отвергает её раньше, чем тест
	// докажет, что дефект ловится и без проверки повтора. Для validateAnchors
	// важен только факт «элементы есть», а он следует из наличия топологии;
	// множество берётся от выравниваний — того же источника, из которого
	// collectElements строит его на валидной карте.
	elements := map[string]bool{}
	for id := range m.AllAlignments() {
		elements[id] = true
	}
	if err := m.validateAnchors(ports, elements); err != nil {
		t.Fatalf("якоря отвергли карту: %v", err)
	}

	// А здесь карта умирает и без проверки повтора: у портов обделённой
	// стрелки нет ни одного прохода.
	err = m.validateAxisIntersections()
	if err == nil {
		t.Fatal("разбор стыков принял карту — значит дефект БЫЛ достижим, и проверка повтора чинит реальную дыру")
	}
	if !strings.Contains(err.Error(), "проходов 0, ожидалось") {
		t.Fatalf("разбор стыков отверг карту по другой причине: %v", err)
	}
	t.Logf("прежнее правило, ловившее дубликат: %v", err)
}
