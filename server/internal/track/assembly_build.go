package track

import (
	"fmt"
	"sort"
)

// assembly_build.go — сборка устройства СОБИРАЕТСЯ ИЗ ТОГО, ЧТО УЕДЕТ НА ПРОВОД.
//
// Не из карты и не из промежуточных величин компилятора, а из RenderGeometry —
// ровно из тех записей, по которым клиент строит тела. Иначе проверка
// доказывала бы смыкание у ВТОРОЙ модели, а поехала бы первая: это в точности та
// болезнь, ради которой файл заведён, только этажом выше.
//
// Отсюда же граница шага (ClearAhead-ax7m.1): здесь ещё нет ни сечений, ни
// состояний стрелки, ни зависящих от положения обязательств. Есть один вопрос —
// «есть ли у внутреннего порта к чему примкнуть» — и он на нынешних данных
// отвечается положением рабочей грани, без сечения и без меша.

// AssembleTurnout собирает детали одного устройства из рецепта отрисовки.
//
// Объявленных разрывов здесь не заводится НИ ОДНОГО, и это не забывчивость.
// Желоб крестовины, изолирующий стык и температурный зазор — намерение автора, и
// объявлять их вправе тот, кто их задумал. Пока их никто не задумал, всякий
// разрыв считается неназванным, и перечень несмыканий честно показывает, сколько
// работы стоит за шагом «стрелка целиком» (ClearAhead-ax7m.3).
func AssembleTurnout(rg *RenderGeometry, els map[string]Element, owner string) (Assembly, error) {
	a := Assembly{Owner: owner}

	half, err := turnoutHalfGauge(rg, owner)
	if err != nil {
		return Assembly{}, err
	}

	// Проходы берутся у ЭЛЕМЕНТОВ по их роли, а не собираются из суффиксов имени:
	// роль — единственная запись о том, чей это проход, и второй сборки имён
	// проект не держит (тот же довод, что в blade.go).
	var passages []RenderElement
	for _, e := range rg.Elements {
		if e.Role != nil && e.Role.Turnout == owner {
			passages = append(passages, e)
		}
	}
	if len(passages) == 0 {
		return Assembly{}, fmt.Errorf("track: сборка %s: у устройства нет ни одного прохода среди элементов", owner)
	}
	sort.Slice(passages, func(i, j int) bool { return passages[i].ID < passages[j].ID })

	for _, p := range passages {
		el, ok := els[p.ID]
		if !ok {
			return Assembly{}, fmt.Errorf("track: сборка %s: проход %s не скомпилирован", owner, p.ID)
		}
		length := el.Prof.LengthU().Meters()
		// Вид детали различается по ветви: нитки ПРЯМОГО прохода и есть рамные
		// рельсы — они лежат от острия до конца перевода и никуда не деваются
		// (blade.go). Называть их просто ниткой значило бы потерять это знание
		// ровно там, где оно понадобится крепежу и переводному механизму.
		kind := PartRail
		if p.Role.Branch == "straight" {
			kind = PartStockRail
		}
		for _, side := range []float64{+half, -half} {
			for i, iv := range aliveSpans(length, gapsOn(rg, owner, p.ID, side)) {
				a.Parts = append(a.Parts, Part{
					ID:       fmt.Sprintf("%s|%s|%+.3f|%d", p.ID, kind, side, i),
					Kind:     kind,
					Owner:    owner,
					Element:  p.ID,
					FromU:    iv[0],
					ToU:      iv[1],
					FaceFrom: side,
					FaceTo:   side,
				})
			}
		}
	}

	for _, b := range rg.TurnoutBlades {
		if b.Owner != owner || b.Length <= 0 {
			continue
		}
		// Остряк ПРИЖАТ: в этом положении его рабочая грань совпадает с гранью
		// рамного рельса, и именно оно проверяется. Отведённый остряк смыкания в
		// острие не имеет по устройству, и требовать его значило бы объявить
		// дефектом сам перевод. Обязательство, зависящее от положения, приезжает
		// вместе с состояниями (ClearAhead-ax7m.3).
		a.Parts = append(a.Parts, Part{
			ID:       fmt.Sprintf("%s|%s", b.Passage, PartBlade),
			Kind:     PartBlade,
			Owner:    owner,
			Element:  b.Passage,
			FromU:    0,
			ToU:      b.Length,
			FaceFrom: b.Offset,
			FaceTo:   b.Offset,
		})
	}

	for i, r := range rg.TurnoutRails {
		if r.Owner != owner {
			continue
		}
		kind := PartWingRail
		switch r.Kind {
		case FrogRailCheck:
			kind = PartCheckRail
		case FrogRailCasting:
			kind = PartFrogCasting
		}
		a.Parts = append(a.Parts, Part{
			ID:       fmt.Sprintf("%s|%s|%d", r.Element, kind, i),
			Kind:     kind,
			Owner:    owner,
			Element:  r.Element,
			FromU:    r.From,
			ToU:      r.To,
			FaceFrom: r.Face,
			FaceTo:   r.EndFace,
		})
	}

	sort.Slice(a.Parts, func(i, j int) bool { return a.Parts[i].ID < a.Parts[j].ID })
	return a, nil
}

// turnoutHalfGauge — полуколея устройства, взятая у ТИПА ПУТИ его прохода.
//
// Оттуда же, откуда её берёт клиент, и это требование, а не удобство: два
// разных ответа на «какая здесь колея» развели бы проверку и то, что она
// проверяет.
func turnoutHalfGauge(rg *RenderGeometry, owner string) (float64, error) {
	var typeID string
	for _, e := range rg.Elements {
		if e.Role != nil && e.Role.Turnout == owner {
			typeID = e.Role.Type
			break
		}
	}
	if typeID == "" {
		return 0, fmt.Errorf("track: сборка %s: у проходов устройства не назван тип пути", owner)
	}
	for _, t := range rg.TrackTypes {
		if t.ID == typeID {
			if t.Gauge <= 0 {
				return 0, fmt.Errorf("track: сборка %s: у типа %s колея %v", owner, typeID, t.Gauge)
			}
			return t.Gauge / 2, nil
		}
	}
	return 0, fmt.Errorf("track: сборка %s: тип пути %s не найден среди отдаваемых", owner, typeID)
}

// gapsOn — разрывы одной нитки: того же элемента и того же выноса.
func gapsOn(rg *RenderGeometry, owner, element string, side float64) [][2]float64 {
	var out [][2]float64
	for _, g := range rg.TurnoutRailGaps {
		if g.Owner != owner || g.Element != element {
			continue
		}
		// Вынос сравнивается с допуском смыкания, а не байт в байт: обе стороны
		// считают его из одной колеи, но через разные выражения.
		if d := g.Offset - side; d > RunningFaceTol || d < -RunningFaceTol {
			continue
		}
		out = append(out, [2]float64{g.From, g.To})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// aliveSpans — куски нитки, на которых она ЕСТЬ: [0, length] минус разрывы.
//
// Вырожденный кусок (короче допуска) отбрасывается: он не деталь, а след
// округления двух соседних разрывов, и порт у него был бы вымышленным.
func aliveSpans(length float64, gaps [][2]float64) [][2]float64 {
	var out [][2]float64
	at := 0.0
	for _, g := range gaps {
		if g[0] > at+RunningFaceTol {
			out = append(out, [2]float64{at, min(g[0], length)})
		}
		if g[1] > at {
			at = g[1]
		}
	}
	if at < length-RunningFaceTol {
		out = append(out, [2]float64{at, length})
	}
	return out
}
