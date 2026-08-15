package track

import (
	"fmt"
	"math"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// drive.go — ПЕРЕВОДНОЙ МЕХАНИЗМ СТРЕЛКИ: где он стоит и какой он.
//
// # Что здесь считается, а что нет
//
// Считается МЕСТО: у какого прохода, на каком u и на сколько в сторону от оси
// стоит станина привода вместе с указателем и табличкой. Место — факт о
// станции: два клиента, каждый выводящий его из топологии сам, дадут два разных
// ответа, и это ровно тот довод, по которому сервер считает крестовину (§5) и
// брусья (timbers.go).
//
// НЕ считается ТЕЛО: высота станины, размер противовеса, толщина щитка
// указателя, размер таблички. Это решения художника того же класса, что длина
// крыла крестовины (контракт отрисовки §1, таблица владения): пока привод —
// нарисованный клиентом предмет, а не ассет, его размеры принадлежат клиенту.
// День, когда появится каталог ассетов, заберёт их у него вместе с крестовиной.
//
// # Сторона выбирается, а не пишется автором
//
// Привод стоит С НАРУЖНОЙ стороны, противоположной сходу: между расходящимися
// путями его ставить некуда — там сходятся два габарита, и человек, который
// переводит стрелку рукой, стоял бы в междупутье.
//
// Сторона выведена ИЗ ГЕОМЕТРИИ (расхождение осей), а не из t.Hand, и это то же
// решение, что в timbers.go: рукость — авторская пометка о том, куда уходит
// боковой путь, а брусья и привод обязаны согласоваться с тем, куда он уходит
// НА САМОМ ДЕЛЕ. Разойдись они — привод встал бы посреди бокового пути, и
// причину пришлось бы искать в двух источниках вместо одного.
//
// Расхождение берётся В КОНЦЕ устройства, а не у острия: у острия оно
// миллиметровое (на марке 1/9 и радиусе 300 м — 7 мм на двух метрах), и знак
// такой величины перевернулся бы от первой же правки арифметики. В конце
// перевода оно около двух метров, и знак у него однозначен.

// DriveAtU — где вдоль прямого прохода стоит станина привода, метры от острия.
//
// ОБЪЯВЛЕННАЯ ПОСТАНОВКА, а не норма: настоящее место привода задаётся эпюрой
// перевода вместе с длиной остряков, а эпюры в карте нет — есть правило (см.
// timbers.go). Два метра ставят станину у корня остряков и не выносят её за
// первый десяток брусьев ни у одной марки.
//
// Число НАЗВАНО ЗДЕСЬ, а не спрятано в функции, чтобы объявленная величина была
// видна списком, — так же, как это сделано у клиента с длиной крыла крестовины.
const DriveAtU = 2.0

// DriveClearance — на сколько станина отнесена от конца бруса, метры.
//
// ОЦЕНКА, и она названа оценкой: привод ставится так, чтобы не мешать подбивке
// и чтобы у стоящего рядом человека была земля под ногами. Полметра за концом
// бруса — это габарит человека, а не измеренная норма.
const DriveClearance = 0.5

// buildTurnoutDrives считает привод каждой стрелки и кладёт его в геометрию.
//
// Порядок вывода — по владельцу: у карты нет причин отдавать стрелки в порядке
// обхода, а хеш геометрии обязан зависеть от содержимого, а не от него.
func buildTurnoutDrives(m *mapfmt.Map, els map[string]Element, rg *RenderGeometry) error {
	c := m.Construction
	if c == nil {
		// Без блока construction нет ни типа, ни длины шпалы — а значит нечем
		// отмерить, куда в сторону отнести станину. Привода нет, и это то же
		// молчание, что у крестовины: чего измерить нечем, того не отдаём.
		return nil
	}
	types := make(map[string]mapfmt.TrackType, len(c.Types))
	for i := range c.Types {
		types[c.Types[i].ID] = c.Types[i]
	}
	for _, t := range m.Topology.Turnouts {
		d, err := turnoutDrive(els, types, c, t)
		if err != nil {
			return fmt.Errorf("track: стрелка %s: %w", mapfmt.Labeled(t.Name, t.ID), err)
		}
		if d != nil {
			rg.TurnoutDrives = append(rg.TurnoutDrives, *d)
		}
	}
	sort.Slice(rg.TurnoutDrives, func(i, j int) bool {
		return rg.TurnoutDrives[i].Owner < rg.TurnoutDrives[j].Owner
	})
	return nil
}

// turnoutDrive считает привод одной стрелки.
func turnoutDrive(els map[string]Element, types map[string]mapfmt.TrackType,
	c *mapfmt.Construction, t mapfmt.Turnout) (*RenderTurnoutDrive, error) {
	typ := t.Type
	if typ == "" {
		typ = c.DefaultType
	}
	tt, ok := types[typ]
	if !ok {
		return nil, fmt.Errorf("тип %q не разрешается — вынос привода отмерить нечем", typ)
	}

	straightID := t.ID + mapfmt.PassageStraight
	divergingID := t.ID + mapfmt.PassageDiverging
	sEl, okS := els[straightID]
	dEl, okD := els[divergingID]
	if !okS || !okD {
		return nil, fmt.Errorf("проходы %s и %s не скомпилированы",
			mapfmt.Labeled(t.Name, straightID), mapfmt.Labeled(t.Name, divergingID))
	}

	sLen := sEl.Plan.Length().Meters()
	dLen := dEl.Plan.Length().Meters()
	if sLen <= 0 {
		return nil, fmt.Errorf("прямой проход нулевой длины")
	}
	// Знак стороны — по расхождению в конце устройства (разбор в шапке файла).
	spread, err := axisSpread(sEl, sLen, dEl, math.Min(sLen, dLen))
	if err != nil {
		return nil, err
	}
	if spread == 0 {
		return nil, fmt.Errorf("проходы не расходятся вовсе — стороны у привода нет")
	}
	side := -1.0 // левая нормаль положительна: сход вправо ставит привод влево
	if spread < 0 {
		side = 1.0
	}
	at := math.Min(DriveAtU, sLen)
	return &RenderTurnoutDrive{
		Owner:   t.ID,
		Name:    t.Name,
		Drive:   t.Drive,
		Element: straightID,
		U:       at,
		Offset:  side * (tt.Sleeper.Length/2 + DriveClearance),
	}, nil
}
