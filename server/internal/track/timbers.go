package track

import (
	"fmt"
	"math"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// timbers.go — РЕШЁТКА УСТРОЙСТВА: переводные брусья стрелки.
//
// Спека контракта отрисовки §4 относит их к уровню 3 («нерегулярные
// конструкции: отдельный рецепт устройства») и заводит его «только при появлении
// конкретного потребителя». Потребитель появился и назван замером
// (ClearAhead-7kv): на ST_A прогоны покрывают 460.00 м из 593.42, а проходы
// стрелок — ноль. 133.42 м, то есть 22.5 % всего пути, стояли без единой шпалы,
// и у горловины это читается как отсутствие половины решётки.
//
// # Почему считает сервер, а не клиент
//
// Та же граница, что у крестовины (§5), и тот же довод: два клиента, независимо
// выводящих решётку из топологии, дадут два разных ответа. Снесённый спайк
// выводил её сам, и это записано дырой Д8 его разбора. Закон клиента
// (ClearAhead-sjq) выдумывать запрещает — значит либо считает сервер, либо
// решётки нет.
//
// # Почему брусья принадлежат УСТРОЙСТВУ, а не прогону
//
// Решение владельца 2026-08-15, и довод найден в коде клиента, а не выведен:
// track_build.gd ставит шпалу позой ЕЁ СОБСТВЕННОГО элемента, то есть поперёк
// его оси. У перевода все брусья лежат поперёк ПРЯМОГО пути — и те, что под
// боковым тоже. Прогон с переменной длиной бруса, второй рассматривавшийся
// вариант, этой ориентации выразить не может: на ветви брусья встали бы поперёк
// ветви. Он дал бы картинку, но неправдивую.
//
// # Правило длины, и почему в карте его нет
//
// Настоящий перевод описывается эпюрой — поимённым списком брусьев. В карту
// кладётся ПРАВИЛО, а список считается: брус перекрывает оба пути, значит его
// длина есть длина шпалы этого же типа плюс расхождение осей в этом сечении.
// Класть в карту производную — завести второй источник истины (тот же довод, что
// у FormationToRail).
//
// ОБЪЯВЛЕННОЕ УПРОЩЕНИЕ, и оно названо здесь, а не умолчано: расхождение осей
// берётся по ОДНОЙ дуговой координате — точка бокового прохода на том же u, что
// и точка прямого. Строго его следовало бы искать проекцией на нормаль прямого
// пути. Цена посчитана, а не оценена: у марки 1/9 угол схода около 6.3°, косинус
// 0.994, то есть продольная невязка на 33 м устройства — 0.2 м, а расхождение
// растёт на 0.11 м на метр, что даёт ошибку длины бруса 0.02 м. Это вчетверо
// меньше ширины самого бруса.

// buildTurnoutGrids укладывает решётку каждой стрелки и кладёт её в геометрию.
//
// Порядок вывода — по владельцу: у карты нет причин отдавать стрелки в порядке
// обхода, а хеш геометрии обязан зависеть от содержимого, а не от него.
func buildTurnoutGrids(m *mapfmt.Map, els map[string]Element, rg *RenderGeometry) error {
	c := m.Construction
	if c == nil {
		return nil
	}
	types := make(map[string]mapfmt.TrackType, len(c.Types))
	for i := range c.Types {
		types[c.Types[i].ID] = c.Types[i]
	}
	// ПРИВОДЫ ЧИТАЮТСЯ ГОТОВЫМИ, а не считаются заново: брусья под станиной
	// удлиняются до её выноса, и второе вычисление того же выноса разъехалось бы
	// с первым — привод стоял бы рядом с помостом, построенным для него же.
	// Порядок вызовов держит compile.go: сперва привод, потом решётка.
	drives := make(map[string]RenderTurnoutDrive, len(rg.TurnoutDrives))
	for _, d := range rg.TurnoutDrives {
		drives[d.Owner] = d
	}
	for _, t := range m.Topology.Turnouts {
		d, hasDrive := drives[t.ID]
		g, err := turnoutGrid(els, types, c, t, d, hasDrive)
		if err != nil {
			return fmt.Errorf("track: стрелка %s: %w", mapfmt.Labeled(t.Name, t.ID), err)
		}
		if g != nil {
			rg.TurnoutGrids = append(rg.TurnoutGrids, *g)
		}
	}
	sort.Slice(rg.TurnoutGrids, func(i, j int) bool {
		return rg.TurnoutGrids[i].Owner < rg.TurnoutGrids[j].Owner
	})
	return nil
}

// turnoutGrid считает брусья одной стрелки. Привод передаётся, чтобы брусья под
// станиной вышли длиннее прочих; hasDrive = false означает «привода у стрелки
// нет», и тогда решётка считается одним правилом на всю длину.
func turnoutGrid(els map[string]Element, types map[string]mapfmt.TrackType,
	c *mapfmt.Construction, t mapfmt.Turnout,
	drive RenderTurnoutDrive, hasDrive bool) (*RenderTurnoutGrid, error) {
	typ := t.Type
	if typ == "" {
		typ = c.DefaultType
	}
	tt, ok := types[typ]
	if !ok {
		return nil, fmt.Errorf("тип %q не разрешается — эпюру брусьев взять неоткуда", typ)
	}
	// Валидатор карты это уже отверг бы (construction.go), но компилятор не
	// полагается на то, что его позвали после него: скомпилировать стрелку без
	// решётки значило бы вернуть ту самую дыру, ради которой файл заведён.
	if tt.Timber == nil {
		return nil, fmt.Errorf("у типа %s нет блока timber", mapfmt.Labeled(tt.Name, typ))
	}
	tb := tt.Timber

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

	g := RenderTurnoutGrid{
		Owner:   t.ID,
		Element: straightID,
		Type:    typ,
		Width:   tb.Width,
		Height:  tb.Height,
		Timbers: []RenderTimber{},
	}

	// Правило диапазона ПОЛУОТКРЫТОЕ и взято у прогона (§4): станция в конечной
	// точке не ставится. Иначе на стыке устройства с ребром брус и первая шпала
	// прогона сошлись бы в одной точке — то самое сдваивание, которое §4
	// запрещает у run'ов и которому здесь неоткуда взяться иначе.
	for n := 0; ; n++ {
		u := float64(n) * tb.Pitch
		if u >= sLen {
			break
		}
		// Боковой проход короче прямого (на ST_A 33.21 против 33.50), и за его
		// концом расхождение брать неоткуда. Держим последнее известное: устройство
		// там уже кончилось, а разница — меньше одного шага.
		du := math.Min(u, dLen)
		spread, err := axisSpread(sEl, u, dEl, du)
		if err != nil {
			return nil, err
		}
		// ПОД ПРИВОДОМ БРУС ДЛИННЕЕ. Требуемый вылет считается со стороны станины
		// — той, что противоположна сходу, — и только у брусьев, попавших в окно
		// вокруг неё. Ноль означает «привода тут нет» и правило не меняет.
		reach := 0.0
		if hasDrive && math.Abs(u-drive.U) <= DriveTimberWindow {
			reach = math.Abs(drive.Offset) + DriveTimberReach
		}
		length, offset := timberSpan(spread, tt.Sleeper.Length, tb.LengthMax, reach)
		g.Timbers = append(g.Timbers, RenderTimber{
			U:      u,
			Length: length,
			Offset: offset,
		})
	}
	return &g, nil
}

// axisSpread — расхождение осей в сечении: смещение точки бокового прохода от
// оси прямого, по ЛЕВОЙ нормали. Знак значим — он говорит, в какую сторону
// растёт брус, и у левой стрелки он противоположен правой.
func axisSpread(sEl Element, su float64, dEl Element, du float64) (float64, error) {
	sd, err := units.MetersToDistance(su)
	if err != nil {
		return 0, fmt.Errorf("прямой проход, u = %g м: %w", su, err)
	}
	dd, err := units.MetersToDistance(du)
	if err != nil {
		return 0, fmt.Errorf("боковой проход, u = %g м: %w", du, err)
	}
	sp, err := sEl.Plan.PoseAt(sEl.Start.Plan, sd)
	if err != nil {
		return 0, fmt.Errorf("поза прямого прохода на u = %g м: %w", su, err)
	}
	dp, err := dEl.Plan.PoseAt(dEl.Start.Plan, dd)
	if err != nil {
		return 0, fmt.Errorf("поза бокового прохода на u = %g м: %w", du, err)
	}
	// Левая нормаль прямого прохода — та же, по которой считаются нитки (§5) и
	// по которой клиент ориентирует шпалу. Одно правило на весь контракт.
	nx, ny := -math.Sin(sp.Heading), math.Cos(sp.Heading)
	return (dp.X-sp.X)*nx + (dp.Y-sp.Y)*ny, nil
}

// timberSpan — длина бруса и смещение его ЦЕНТРА от оси прямого прохода.
//
// Брус лежит от дальнего края прямого пути (со стороны, противоположной сходу)
// до дальнего края бокового. Ближний край поэтому НЕПОДВИЖЕН — он привязан к
// прямому пути, — и упор в length_max растит брус только в сторону схода, а не в
// обе. Растяни его симметрично, и под прямым путём кончился бы вылет: у
// длиннейших брусьев рельс встал бы на самый торец.
//
// reach — насколько далеко брус обязан уйти в сторону, ПРОТИВОПОЛОЖНУЮ сходу:
// столько требует привод, чтобы станина стояла на брусе, а не за его концом
// (разбор — у DriveTimberReach). Ноль означает «под этим брусом привода нет», и
// тогда ближний край остаётся на половине шпалы, как у всех прочих.
//
// УПОР В LENGTH_MAX РЕЖЕТ СО СТОРОНЫ СХОДА, а не со стороны привода, и это
// выбор, а не побочность: боковой путь при обрезке теряет вылет бруса, привод —
// опору целиком. Первое видно как короткий брус, второе — как ящик, висящий над
// откосом. Цена названа здесь, потому что на карте с длинным выносом и коротким
// length_max это станет заметно.
func timberSpan(spread, sleeperLength, lengthMax, reach float64) (length, offset float64) {
	sign := 1.0
	if spread < 0 {
		sign = -1.0
	}
	half := sleeperLength / 2
	nearHalf := math.Max(half, reach) // докуда брус уходит от оси в сторону привода
	near := -sign * nearHalf          // край со стороны прямого пути
	far := sign * (half + math.Abs(spread))
	full := nearHalf + half + math.Abs(spread)
	if full <= lengthMax {
		return full, (near + far) / 2
	}
	return lengthMax, near + sign*lengthMax/2
}
