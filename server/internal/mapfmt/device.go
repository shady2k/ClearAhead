package mapfmt

import "maps"

// Устройство и его проходы.
//
// # Зачем этот файл
//
// До 2026-08-11 набор проходов стрелки был записан РУКАМИ В ТРЁХ МЕСТАХ:
// intersect.go строил ends[], propagate.go строил свой ends[], compile.go
// строил passage[]. Каждое перечисляло одно и то же — «проход :straight идёт от
// common к straight, проход :diverging от common к diverging», — и каждое
// пришлось бы править, чтобы появилось устройство с другим числом портов.
//
// Здесь это записано один раз.
//
// # Что обобщено, а что нет
//
// Обобщены ПОРТЫ и ПЕРЕХОДЫ: их число не зашито, и глухое пересечение с
// четырьмя портами и двумя непересекающимися проходами выражается той же
// формой, что обыкновенная стрелка. Это снимает ограничение, из-за которого
// перекрёстный съезд, глухое пересечение и сбрасыватель были невыразимы
// (map-content-design §4).
//
// НЕ обобщены состояния и конфликты переходов. У них сегодня нет ни одного
// потребителя: положение остряка разрешает переход только в замыкании
// маршрута, а централизации в проекте ещё нет. Объявить их сейчас значило бы
// нарушить правило map-format-design §8 — «объявленная форма без потребителя
// окажется неверной». Они входят вместе с централизацией.
//
// В ФАЙЛЕ карты стрелка остаётся трёхпортовой записью: рукой пишут её, а не
// таблицу переходов. Обобщена форма, которую видит код, а не форма, которую
// пишет автор.

// Passage — один проход устройства: адресуемый линейный элемент между двумя
// портами.
//
// Проходы адресуемы (спека §8): без этого остряк, крестовина и предельный
// столбик внутри стрелки безадресны, а их длины не попадают в TrackPos.
type Passage struct {
	// ID — идентификатор прохода как линейного элемента: ST_A_SW_1:straight.
	ID string
	// From и To — КВАЛИФИЦИРОВАННЫЕ порты вида ST_A_SW_1.C.
	From string
	To   string
	// Branch — роль прохода у стрелки: "straight" или "diverging". У
	// устройства без ветвления пусто.
	Branch string
}

// Passages — проходы стрелки. Единственное место, где записано, какие проходы у
// неё есть и между какими портами они идут.
//
// Порядок детерминирован: прямой проход, затем боковой. От порядка зависит
// хеш компиляции, поэтому он часть контракта, а не деталь.
func (t Turnout) Passages() []Passage {
	q := func(port string) string { return t.ID + "." + port }
	return []Passage{
		{
			ID:     t.ID + PassageStraight,
			From:   q(t.Ports.Common),
			To:     q(t.Ports.Straight),
			Branch: "straight",
		},
		{
			ID:     t.ID + PassageDiverging,
			From:   q(t.Ports.Common),
			To:     q(t.Ports.Diverging),
			Branch: "diverging",
		},
	}
}

// PortIDs — квалифицированные порты стрелки в детерминированном порядке.
func (t Turnout) PortIDs() []string {
	return []string{
		t.ID + "." + t.Ports.Common,
		t.ID + "." + t.Ports.Straight,
		t.ID + "." + t.Ports.Diverging,
	}
}

// PassageEnds — концы всех проходов всех устройств карты: ID прохода в пару
// портов. Форма выбрана под вызывающих, которые уже держат такую же карту для
// рёбер и хотят одну таблицу на то и другое.
func (m *Map) PassageEnds() map[string][2]string {
	out := make(map[string][2]string, 2*len(m.Topology.Turnouts))
	for _, t := range m.Topology.Turnouts {
		for _, p := range t.Passages() {
			out[p.ID] = [2]string{p.From, p.To}
		}
	}
	return out
}

// ElementEnds — концы ВСЕХ линейных элементов: рёбер и проходов устройств.
func (m *Map) ElementEnds() map[string][2]string {
	out := make(map[string][2]string, len(m.Topology.Edges)+2*len(m.Topology.Turnouts))
	for _, e := range m.Topology.Edges {
		out[e.ID] = [2]string{e.From, e.To}
	}
	maps.Copy(out, m.PassageEnds())
	return out
}
