package track

import (
	"fmt"
	"math"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// buildTurnoutRunningRails заменяет маршрутные нитки поимёнными физическими
// деталями стрелки. Проходы здесь — только оси адресации: ни один из них не
// получает автоматически пару рельсов.
func buildTurnoutRunningRails(m *mapfmt.Map, els map[string]Element, rg *RenderGeometry) error {
	if m.Construction == nil {
		return nil
	}
	types := make(map[string]mapfmt.TrackType, len(m.Construction.Types))
	for i := range m.Construction.Types {
		types[m.Construction.Types[i].ID] = m.Construction.Types[i]
	}
	for _, t := range m.Topology.Turnouts {
		typeID := t.Type
		if typeID == "" {
			typeID = m.Construction.DefaultType
		}
		tt, ok := types[typeID]
		if !ok {
			return fmt.Errorf("track: стрелка %s: тип %q не разрешается — физические рельсы построить не из чего",
				mapfmt.Labeled(t.Name, t.ID), typeID)
		}
		project, err := mapfmt.TurnoutProjectByID(t.TurnoutType)
		if err != nil {
			return fmt.Errorf("track: стрелка %s: %w", mapfmt.Labeled(t.Name, t.ID), err)
		}
		if err := appendTurnoutRunningRails(rg, els, t, tt, project); err != nil {
			return fmt.Errorf("track: стрелка %s: %w", mapfmt.Labeled(t.Name, t.ID), err)
		}
	}
	sortRails(rg.Rails)
	return nil
}

func appendTurnoutRunningRails(rg *RenderGeometry, els map[string]Element,
	t mapfmt.Turnout, tt mapfmt.TrackType, project mapfmt.TurnoutType) error {
	straightID := t.ID + mapfmt.PassageStraight
	divergingID := t.ID + mapfmt.PassageDiverging
	straight, ok := els[straightID]
	if !ok {
		return fmt.Errorf("прямой проход %s не скомпилирован", straightID)
	}
	diverging, ok := els[divergingID]
	if !ok {
		return fmt.Errorf("боковой проход %s не скомпилирован", divergingID)
	}
	half := tt.Gauge / 2
	var innerStraight, innerDiverging float64
	switch t.Hand {
	case mapfmt.HandRight:
		innerStraight, innerDiverging = -half, +half
	case mapfmt.HandLeft:
		innerStraight, innerDiverging = +half, -half
	default:
		return fmt.Errorf("рукость %q, ожидается right или left", t.Hand)
	}

	bladeLength := map[string]float64{}
	for _, b := range rg.TurnoutBlades {
		if b.Owner == t.ID {
			bladeLength[b.Passage] = b.Length
		}
	}
	if bladeLength[straightID] <= 0 || bladeLength[divergingID] <= 0 {
		return fmt.Errorf("пара остряков не собрана")
	}

	// Внутренняя нитка каждого маршрута вокруг крестовины заменяется усовиком
	// и сердечником. Границы берутся у самих деталей, а не повторяются числами.
	//
	// КОНЕЦ РАЗРЫВА — ПО ОБОИМ, а не только по отливке: усовик и сердечник кончаются
	// в одном сечении (корне), и брать границу у одного из них значило бы молча
	// назначить его главным. Разойдись они когда-нибудь — разрыв поедет за тем, кто
	// кончается позже, а не за тем, кого выбрал считавший.
	frogFrom := map[string]float64{}
	frogTo := map[string]float64{}
	wingContinuous := map[string]string{}
	for _, r := range rg.Rails {
		if r.Owner != t.ID {
			continue
		}
		switch r.Kind {
		case FrogRailWing:
			if old, seen := frogFrom[r.Element]; !seen || r.From < old {
				frogFrom[r.Element] = r.From
			}
			if r.To > frogTo[r.Element] {
				frogTo[r.Element] = r.To
			}
			// Половина ДО горла продолжает внутренний соединительный рельс без
			// стыка металла. Её ключ нужен ниже, чтобы клиент протянул обе записи
			// одним телом; половина за горлом имеет порядок 2 и сюда не попадает.
			if r.ContinuousOrder == 1 {
				wingContinuous[r.Element] = r.ContinuousID
			}
		case FrogRailCasting:
			if r.To > frogTo[r.Element] {
				frogTo[r.Element] = r.To
			}
		}
	}

	straightLength := straight.Plan.Length().Meters()
	divergingLength := diverging.Plan.Length().Meters()
	appendRail := func(id, element, kind string, from, to, face float64) {
		if to <= from+RunningFaceTol {
			return
		}
		rg.Rails = append(rg.Rails, RenderRailPart{
			ID: id, Owner: t.ID, Element: element, Kind: kind,
			From: from, To: to, Face: face, EndFaceFrom: face, EndFaceTo: face,
			Grow: math.Copysign(1, face),
		})
	}
	appendAroundFrog := func(id, element, kind string, from, length, face float64) {
		ff, hasFrom := frogFrom[element]
		ft, hasTo := frogTo[element]
		if !hasFrom || !hasTo || ft <= ff {
			appendRail(id, element, kind, from, length, face)
			return
		}
		beforeAt := len(rg.Rails)
		appendRail(id+"|before_frog", element, kind, from, ff, face)
		if beforeAt < len(rg.Rails) {
			// Соединительный рельс и усовик — одна согнутая рельсовая деталь.
			// Раздельная протяжка клала в их общем сечении два кольца и давала
			// видимый белый клин ровно перед крестовиной.
			rg.Rails[beforeAt].ContinuousID = wingContinuous[element]
			rg.Rails[beforeAt].ContinuousOrder = 0
		}
		appendRail(id+"|after_frog", element, kind, ft, length, face)
	}

	// ГДЕ КОНЧАЕТСЯ РАМНЫЙ РЕЛЬС. Эпюра даёт его длину от ПЕРЕДНЕГО СТЫКА
	// перевода, а проходы начинаются в острие — отсюда вычитание. Число
	// приехало 2026-08-17 от владельца: 12.5 м при острие в 3.531 м от
	// переднего стыка, то есть рамный рельс выходит за корень остряка на
	// 2.469 м и кончается стыком, за которым ту же нитку ведёт соединительный
	// рельс. До этого числа обе нитки шли одной деталью до крестовины: стык,
	// который на перевода есть, в модели отсутствовал.
	//
	// Длина мерится ПО САМОМУ РЕЛЬСУ, а u прохода и есть длина дуги, поэтому оба
	// рамных рельса кончаются в одном u — каждый на своём проходе. Пересчёта
	// пропорцией здесь нет и быть не может: доля длины прохода — не место на
	// перевода (разбор — ниже, у кривого рамного рельса).
	stockEnd := project.Switch.StockRailLength - project.Switch.ToeFromFrontJoint
	if stockEnd <= 0 || stockEnd > straightLength || stockEnd > divergingLength {
		// Проект, у которого рамный рельс не помещается в свой же перевод,
		// строить нечем. Обрезка молчанием была бы правдоподобной подстановкой.
		return fmt.Errorf("рамный рельс кончается в %.3f м от острия при проходах %.3f и %.3f м",
			stockEnd, straightLength, divergingLength)
	}
	outerDiverging := -innerDiverging

	// ДВА РАМНЫХ РЕЛЬСА: ПРЯМОЙ И КРИВОЛИНЕЙНЫЙ, И ВТОРОЙ ЛЕЖИТ НА БОКОВОМ
	// ПРОХОДЕ.
	//
	// Так на схеме перевода, которую владелец показал 2026-08-17 второй раз и
	// крупно: нижний рамный рельс ① идёт от переднего стыка прямо, а за корнем
	// остряков ОТГИБАЕТСЯ и уходит наружной ниткой бокового пути. Прямым
	// остаётся только верхний. Владелец назвал это одной фразой, показав на две
	// наши детали: «вот этот рельс — продолжение этого, а они у тебя разорваны».
	//
	// Предыдущая редакция утверждала обратное — «оба рамных вдоль прямого пути,
	// а наружная нитка бокового начинается за стрелкой», — и была прочтением той
	// же схемы, только мелкой. Цена ошибки померена: наружная нитка начиналась в
	// 0.3546 м вбок от конца рамного рельса, а колея бокового маршрута в пределах
	// стрелки сходила с 1520 мм до 1165 мм — по такому переводу не проехать.
	//
	// ОТСЮДА ЖЕ ИСЧЕЗАЕТ ОБЪЯВЛЕННЫЙ РАЗРЫВ. Он говорил «наружную нитку бокового
	// ведёт рамный рельс прямого пути» — и это правда ровно в том смысле, в
	// каком её ведёт СВОЙ рамный рельс: он и есть эта нитка. Металл непрерывен от
	// острия, объявлять нечего.
	appendRail(t.ID+"|stock|straight", straightID, TurnoutRailStock,
		0, stockEnd, -innerStraight)
	appendRail(t.ID+"|stock|curved", divergingID, TurnoutRailStock,
		0, stockEnd, outerDiverging)

	// СОЕДИНИТЕЛЬНЫЕ РЕЛЬСЫ ЗА РАМНЫМИ — по одному на каждый рамный, встык.
	appendRail(t.ID+"|closure|straight_outer", straightID, TurnoutRailClosure,
		stockEnd, straightLength, -innerStraight)
	appendRail(t.ID+"|closure|diverging_outer", divergingID, TurnoutRailClosure,
		stockEnd, divergingLength, outerDiverging)

	// СОЕДИНИТЕЛЬНЫЕ РЕЛЬСЫ ЗА ОСТРЯКАМИ — по одному на каждый остряк, от корня.
	//
	// Внутренние нитки обоих маршрутов начинаются В КОРНЕ своего остряка, а не за
	// концом рамных рельсов: рамного рельса под ними больше нет — прямой лежит по
	// другому краю колеи, кривой ушёл на боковой проход. Так на схеме: за корнем
	// остряка его продолжает соединительный рельс, и это тот же стык, что у
	// рамного, только раньше.
	appendAroundFrog(t.ID+"|closure|straight_inner", straightID, TurnoutRailClosure,
		bladeLength[straightID], straightLength, innerStraight)
	appendAroundFrog(t.ID+"|closure|diverging_inner", divergingID, TurnoutRailClosure,
		bladeLength[divergingID], divergingLength, innerDiverging)
	return nil
}
