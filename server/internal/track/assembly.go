package track

import (
	"fmt"
	"math"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/units"
)

// assembly.go — СБОРКА: путь как поимённые детали с портами и обязательствами.
//
// # Зачем это заведено
//
// До 2026-08-16 сервер отдавал РЕЦЕПТ: нитки прохода плюс правила вычитания
// (RenderTurnoutRailGap) плюс отдельные списки остряков и ниток крестовины. Тела
// из этого не было — тело собирал клиент. Два следствия, и оба были дырами:
//
//	 1. Единственным местом, где можно спросить «сомкнулся ли металл», оказался
//	    Godot: проверка 44_mesh_shell.gd режет ТРЕУГОЛЬНИКИ плоскостью и меряет
//	    занятые интервалы. Свойство домена проверялось через рендер.
//	 2. Проверка перечисляла ДЕТАЛИ (остряки), а не СТЫКИ, и потому не увидела
//	    нитку, у которой остряка нет вовсе. Замер: у бокового прохода вырезаны обе
//	    нитки на длину остряка, а остряк дан один; наружная нитка возобновляется в
//	    0.1145 м от рамного рельса — при головке 0.075 м это 39.5 мм чистого
//	    воздуха, то есть рельс с открытым торцом, начинающийся ниоткуда.
//
// Третьим доказательством того, что рецепт не держит собственность, служит
// BladeRootJoint (blade.go): накладки, «которыми закрыт стык», объявлены
// константой и разобраны в комментарии — а деталей таких нет ни в данных, ни в
// построении. Соединение существовало в тексте и не существовало в модели.
//
// # Почему список стыков НЕ ОБЪЯВЛЯЕТСЯ автором
//
// Соблазн был прямой: рядом с деталями положить записи вида «A смыкается с B».
// Это завело бы ТРЕТИЙ рецепт, врущий наравне с первыми двумя: ничто не мешает
// такой записи утверждать смычку там, где зазор. Ошибка переехала бы с клиента в
// декларацию сервера и осталась бы ошибкой.
//
// Поэтому здесь наоборот:
//
//	порт ВЫВОДИТСЯ из границы детали — автор его не пишет;
//	смычка ДОКАЗЫВАЕТСЯ расстоянием между портами — автор её не утверждает;
//	объявляется только НАМЕРЕНИЕ разорвать (желоб, изолирующий стык), потому что
//	намерение из геометрии не выводится ничем.
//
// # Почему ValidatedAssembly непрозрачна
//
// Поля неэкспортируемые, конструктор один — Validate. Значения «сборка, которую
// не проверяли» в типах не существует, и потребитель не может его получить,
// забыв позвать проверку. Это то же правило, что «валидатор отказывает, а не
// чинит», перенесённое с ЗНАЧЕНИЙ на ГЕОМЕТРИЮ.
//
// # Словарь взят у IFC 4.3, а не выдуман
//
// STOCKRAIL, BLADE, CHECKRAIL, GUARDRAIL, FROG — типы IfcRail и
// IfcTrackElementTypeEnum; ATSTART/ATEND — виды примыкания
// IfcRelConnectsPathElements. Разбор приор арта (2026-08-16) показал, что
// словарь у отрасли есть, а доказательства смычки нет ни у кого: у Trainz,
// OpenRails, Cities:Skylines и OpenDRIVE стык означает «поезд может перейти», а
// не «ходовая поверхность сомкнулась». Словарь берём готовым, доказательство
// делаем сами.

// Виды деталей. Строки — словарь IFC 4.3, чтобы имя не пришлось переводить
// дважды при первом же обмене.
const (
	PartRail        = "rail"
	PartStockRail   = "stock_rail"
	PartBlade       = "blade"
	PartWingRail    = "wing_rail"
	PartCheckRail   = "check_rail"
	PartFrogCasting = "frog_casting"
)

// Концы детали. Те же два слова, что у IfcRelConnectsPathElements.
const (
	PortAtStart = "start"
	PortAtEnd   = "end"
)

// RunningFaceTol — допуск смыкания РАБОЧИХ ГРАНЕЙ, метры.
//
// Миллиметр, и это не оценка точности счёта: обе точки считаются из одной оси
// одной арифметикой, и разойтись на большее они могут только потому, что модель
// их развела. Настоящий рельсовый стык имеет температурный зазор до ~20 мм — но
// он ОБЪЯВЛЕН, и объявленный разрыв проверку проходит отдельным путём. Здесь
// проверяется необъявленное, а необъявленного зазора не бывает.
const RunningFaceTol = 0.001

// Part — деталь сборки: протяжка вдоль интервала элемента.
//
// FaceFrom/FaceTo — вынос РАБОЧЕЙ ГРАНИ по левой нормали на концах интервала.
// Именно грань, а не ось детали: смыкаются рабочие грани, по ним катится
// колесо, и сравнивать оси значило бы объявить смычку у двух рельсов разной
// ширины головки.
//
// Сечения здесь ещё нет. Оно приезжает следующим шагом (ClearAhead-ax7m.2,
// строжка остряка): нынешняя проверка спрашивает про положение грани, и для
// этого вопроса сечение не нужно. Заводить поле, которым никто не пользуется,
// значило бы обещать выраженность, которой нет.
type Part struct {
	ID       string
	Kind     string
	Owner    string
	Element  string
	FromU    float64
	ToU      float64
	FaceFrom float64
	FaceTo   float64
}

// Port — конец детали. ВЫВОДИТСЯ, а не задаётся.
//
// Конструктора у Port снаружи нет намеренно: единственный способ его получить —
// спросить у детали (Part.ports). Порт, написанный руками, был бы ровно тем
// объявлением, ради отказа от которого файл и заведён.
type Port struct {
	Part    string
	Kind    string
	End     string
	Element string
	U       float64
	// X, Y, Z — точка РАБОЧЕЙ ГРАНИ в координатах региона.
	X, Y, Z float64
	// Interior — порт лежит ВНУТРИ элемента, а не на его конце.
	//
	// Различие несёт всю силу проверки. Порт на конце элемента законно бывает
	// свободным: там кончается ребро, и что дальше — вопрос топологии сети, а не
	// сборки устройства. Порт ВНУТРИ элемента свободным не бывает никогда: он
	// возник потому, что деталь здесь кончилась, и обязан назвать, что дальше.
	Interior bool
}

// Gap — ОБЪЯВЛЕННЫЙ разрыв: место, где металла нет НАРОЧНО.
//
// Единственное, что в этой модели объявляется автором, и объявляется по
// необходимости: желоб крестовины, изолирующий стык и температурный зазор из
// геометрии не выводятся ничем — это намерение, а намерения в числах нет.
//
// Вид назван отдельным словом, а не одним «разрывом»: у желоба и у изолирующего
// стыка разные последствия для ведения поезда, и однажды их придётся различать.
type Gap struct {
	Kind    string // flangeway | insulated | thermal
	Element string
	Face    float64
	From    float64
	To      float64
	Why     string
}

// Break — НЕСМЫКАНИЕ: порт, которому не к чему примкнуть.
//
// Возвращается перечнем, а не первым найденным: перечень несмыканий — это и
// есть список работ, а отказ по первому скрыл бы остальные до следующего
// запуска.
type Break struct {
	Port Port
	// Nearest — ближайший чужой порт и расстояние до него. Пустой Part значит,
	// что чужих портов нет вовсе.
	Nearest  Port
	Distance float64
}

// Assembly — детали одного устройства и объявленные разрывы.
type Assembly struct {
	Owner string
	Parts []Part
	Gaps  []Gap
}

// ValidatedAssembly — сборка, ПРОШЕДШАЯ проверку.
//
// Поля неэкспортируемые, конструктор один. Значения «непроверенная сборка» в
// типах не существует — потребитель не может забыть позвать проверку, потому
// что без неё у него не будет чего потреблять.
type ValidatedAssembly struct {
	owner string
	parts []Part
	ports []Port
}

// Owner называет устройство проверенной сборки.
func (v *ValidatedAssembly) Owner() string { return v.owner }

// Parts отдаёт КОПИЮ деталей: проверенная сборка не вправе измениться после
// проверки, а общий срез отдал бы потребителю доступ в её внутренности.
func (v *ValidatedAssembly) Parts() []Part {
	out := make([]Part, len(v.parts))
	copy(out, v.parts)
	return out
}

// Ports отдаёт копию выведенных портов — по тому же доводу.
func (v *ValidatedAssembly) Ports() []Port {
	out := make([]Port, len(v.ports))
	copy(out, v.ports)
	return out
}

// Validate доказывает сборку и только при успехе отдаёт её значение.
//
// Правило полноты: ВНУТРЕННИЙ порт обязан либо сомкнуться с чужим портом в
// пределах RunningFaceTol, либо попасть в объявленный разрыв. Третьего не дано,
// и «ничего» третьим не считается — именно «ничего» и стояло на корне остряка.
func Validate(a Assembly, els map[string]Element) (*ValidatedAssembly, error) {
	ports, err := derivePorts(a, els)
	if err != nil {
		return nil, err
	}
	breaks := findBreaks(ports, a.Gaps)
	if len(breaks) > 0 {
		return nil, breaksError(a.Owner, breaks)
	}
	return &ValidatedAssembly{owner: a.Owner, parts: append([]Part(nil), a.Parts...), ports: ports}, nil
}

// Breaks находит несмыкания, НЕ отказывая.
//
// Заведено ради разбора, а не ради обхода отказа: перечень несмыканий на
// нынешней модели — это список работ следующего шага, и его надо уметь
// напечатать, не роняя сборку. Потребителям геометрии эта дорога закрыта: она
// не отдаёт ValidatedAssembly.
func Breaks(a Assembly, els map[string]Element) ([]Break, error) {
	ports, err := derivePorts(a, els)
	if err != nil {
		return nil, err
	}
	return findBreaks(ports, a.Gaps), nil
}

// derivePorts выводит по два порта на деталь. Порядок канонический: детали по
// ID, у детали сперва начало, потом конец — сборка попадёт в хеш геометрии, а
// обход Go-map недетерминирован.
func derivePorts(a Assembly, els map[string]Element) ([]Port, error) {
	parts := append([]Part(nil), a.Parts...)
	sort.Slice(parts, func(i, j int) bool { return parts[i].ID < parts[j].ID })

	out := make([]Port, 0, len(parts)*2)
	for _, p := range parts {
		el, ok := els[p.Element]
		if !ok {
			return nil, fmt.Errorf("track: сборка %s: деталь %s ссылается на элемент %s, которого нет среди скомпилированных",
				a.Owner, p.ID, p.Element)
		}
		lengthU := el.Prof.LengthU().Meters()
		for _, end := range []string{PortAtStart, PortAtEnd} {
			u, face := p.FromU, p.FaceFrom
			if end == PortAtEnd {
				u, face = p.ToU, p.FaceTo
			}
			x, y, z, err := facePoint(el, u, face)
			if err != nil {
				return nil, fmt.Errorf("track: сборка %s: деталь %s, конец %s: %w", a.Owner, p.ID, end, err)
			}
			out = append(out, Port{
				Part: p.ID, Kind: p.Kind, End: end, Element: p.Element, U: u,
				X: x, Y: y, Z: z,
				// Допуск сравнения с концом элемента — тот же миллиметр: u приходит
				// из счёта, и «ровно ноль» здесь означало бы сравнение float64 байт
				// в байт, что проект уже терял.
				Interior: u > RunningFaceTol && u < lengthU-RunningFaceTol,
			})
		}
	}
	return out, nil
}

// facePoint — точка рабочей грани на элементе: поза оси плюс вынос по ЛЕВОЙ
// нормали. Та же адресация, что у брусьев, привода и разрывов, — второго языка
// «где это поперёк пути» проект не держит.
func facePoint(el Element, u, face float64) (x, y, z float64, err error) {
	du, err := units.MetersToDistance(u)
	if err != nil {
		return 0, 0, 0, err
	}
	plan, err := el.Plan.PoseAt(el.Start.Plan, du)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("план на %v м: %w", u, err)
	}
	rise, _, err := el.Prof.At(du)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("профиль на %v м: %w", u, err)
	}
	nx, ny := -math.Sin(plan.Heading), math.Cos(plan.Heading)
	return plan.X + nx*face, plan.Y + ny*face, el.Start.Z + rise, nil
}

// findBreaks ищет внутренние порты, которым не к чему примкнуть.
func findBreaks(ports []Port, gaps []Gap) []Break {
	var out []Break
	for i, p := range ports {
		if !p.Interior || coveredByGap(p, gaps) {
			continue
		}
		best := -1
		bestD := math.Inf(1)
		for j, q := range ports {
			// Свои же порты не считаются: у детали два конца, и «примкнула сама к
			// себе» смычкой не является.
			if i == j || q.Part == p.Part {
				continue
			}
			d := math.Hypot(math.Hypot(p.X-q.X, p.Y-q.Y), p.Z-q.Z)
			if d < bestD {
				best, bestD = j, d
			}
		}
		if best >= 0 && bestD <= RunningFaceTol {
			continue
		}
		b := Break{Port: p, Distance: bestD}
		if best >= 0 {
			b.Nearest = ports[best]
		}
		out = append(out, b)
	}
	return out
}

// coveredByGap — попадает ли порт в объявленный разрыв. Грань сравнивается по
// той же левой нормали и с тем же допуском: разрыв объявляют ниткой, а не
// областью.
func coveredByGap(p Port, gaps []Gap) bool {
	for _, g := range gaps {
		if g.Element != p.Element {
			continue
		}
		if p.U < g.From-RunningFaceTol || p.U > g.To+RunningFaceTol {
			continue
		}
		return true
	}
	return false
}

// breaksError собирает отказ. Текст называет расстояние и ближайшего соседа:
// без них отказ сообщает «где-то не сошлось» и разбирать его приходится
// снимком, то есть ровно тем способом, от которого файл уводит.
func breaksError(owner string, breaks []Break) error {
	sort.Slice(breaks, func(i, j int) bool {
		if breaks[i].Port.Part != breaks[j].Port.Part {
			return breaks[i].Port.Part < breaks[j].Port.Part
		}
		return breaks[i].Port.End < breaks[j].Port.End
	})
	msg := fmt.Sprintf("track: сборка %s: несомкнутых портов %d", owner, len(breaks))
	for _, b := range breaks {
		if b.Nearest.Part == "" {
			msg += fmt.Sprintf("\n  %s[%s] на %s u=%.3f: примыкать не к чему — других портов нет",
				b.Port.Part, b.Port.End, b.Port.Element, b.Port.U)
			continue
		}
		msg += fmt.Sprintf("\n  %s[%s] на %s u=%.3f: ближайший металл %s[%s] в %.4f м (допуск %.4f)",
			b.Port.Part, b.Port.End, b.Port.Element, b.Port.U,
			b.Nearest.Part, b.Nearest.End, b.Distance, RunningFaceTol)
	}
	return fmt.Errorf("%s", msg)
}
