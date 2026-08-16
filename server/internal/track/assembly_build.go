package track

import (
	"fmt"
	"math"
	"sort"
)

// assembly_build.go — сборка устройства СОБИРАЕТСЯ ИЗ ТОГО, ЧТО УЕДЕТ НА ПРОВОД.
//
// Не из карты и не из промежуточных величин компилятора, а из RenderGeometry —
// ровно из тех записей, по которым клиент строит тела. Иначе проверка
// доказывала бы смыкание у ВТОРОЙ модели, а поехала бы первая: это в точности та
// болезнь, ради которой файл заведён, только этажом выше.
//
// Вопросов теперь два, и оба отвечаются арифметикой, без единого треугольника:
// «есть ли внутреннему порту к чему примкнуть» (положение рабочей грани) и
// «продолжено ли ТЕЛО» (занятый поперечный отрезок). Второй появился, как только
// переменное сечение переехало на сервер (ClearAhead-ax7m.2): до того форму
// остряка выбирал клиент, и спрашивать про неё здесь было не у кого.
//
// Чего здесь по-прежнему НЕТ: состояний стрелки и зависящих от положения
// обязательств. Проверяется ПРИЖАТЫЙ остряк — то положение, в котором он и есть
// путь под колесом. У отведённого смыкания в острие нет по устройству, и
// требовать его значило бы объявить дефектом сам перевод.

// AssembleTurnout собирает детали одного устройства из рецепта отрисовки.
//
// Объявленных разрывов здесь не заводится НИ ОДНОГО, и это не забывчивость.
// Желоб крестовины, изолирующий стык и температурный зазор — намерение автора, и
// объявлять их вправе тот, кто их задумал. Пока их никто не задумал, всякий
// разрыв считается неназванным, и перечень несмыканий честно показывает, сколько
// работы стоит за шагом «стрелка целиком» (ClearAhead-ax7m.3).
func AssembleTurnout(rg *RenderGeometry, els map[string]Element, owner string) (Assembly, error) {
	a := Assembly{Owner: owner}

	tt, err := turnoutTrackType(rg, owner)
	if err != nil {
		return Assembly{}, err
	}
	if tt.Gauge <= 0 {
		return Assembly{}, fmt.Errorf("track: сборка %s: у типа %s колея %v", owner, tt.ID, tt.Gauge)
	}
	half := tt.Gauge / 2
	near, far := railSpan(tt.Rail)
	head := tt.Rail.HeadWidth

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
					// Нитка растёт ОТ ОСИ НАРУЖУ: рабочая грань смотрит внутрь
					// колеи, там едет колесо, и тело уходит в другую сторону.
					// Знак выноса и есть эта сторона — второго правила не нужно.
					Grow:      math.Copysign(1, side),
					Near:      near,
					Far:       far,
					Head:      head,
					ScaleFrom: 1,
					ScaleTo:   1,
				})
			}
		}
	}

	for _, b := range rg.TurnoutBlades {
		if b.Owner != owner || b.Length <= 0 {
			continue
		}
		bladeNear, bladeFar := railSpan(b.Rail)
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
			// Сторона роста ПРИСЛАНА: с 2026-08-16 остряк растёт НАРУЖУ, как
			// всякий рельс, — разбор в blade.go.
			Grow: b.Grow,
			// ПРОФИЛЬ СВОЙ, ОСТРЯКОВЫЙ. Остряк катают из ОР65, и мерить его тело
			// путевым Р65 значило бы проверять форму, которой клиент не строит:
			// у ОР65 подошва уже на 17 мм и высота меньше на 40.
			Near: bladeNear,
			Far:  bladeFar,
			Head: b.Rail.HeadWidth,
			// Строжка: в острие сечение уже в разы. Доли берутся из той же
			// таблицы, что уехала на провод, — второго её чтения не заводится.
			ScaleFrom: bladeScaleAt(b.Section, 0, bladeFar),
			ScaleTo:   bladeScaleAt(b.Section, b.Length, bladeFar),
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
		a.Parts = append(a.Parts, flaredParts(fmt.Sprintf("%s|%s|%d", r.Element, kind, i),
			kind, owner, r, near, far, head)...)
	}

	sort.Slice(a.Parts, func(i, j int) bool { return a.Parts[i].ID < a.Parts[j].ID })
	return a, nil
}

// turnoutTrackType — тип пути устройства, взятый у роли его прохода.
//
// Оттуда же, откуда его берёт клиент, и это требование, а не удобство: два
// разных ответа на «какая здесь колея» развели бы проверку и то, что она
// проверяет.
func turnoutTrackType(rg *RenderGeometry, owner string) (RenderTrackType, error) {
	var typeID string
	for _, e := range rg.Elements {
		if e.Role != nil && e.Role.Turnout == owner {
			typeID = e.Role.Type
			break
		}
	}
	if typeID == "" {
		return RenderTrackType{}, fmt.Errorf("track: сборка %s: у проходов устройства не назван тип пути", owner)
	}
	for _, t := range rg.TrackTypes {
		if t.ID == typeID {
			return t, nil
		}
	}
	return RenderTrackType{}, fmt.Errorf("track: сборка %s: тип пути %s не найден среди отдаваемых", owner, typeID)
}

// railSpan — пределы сечения рельса от рабочей грани наружу.
//
// Берётся ПРИСЛАННОЕ сечение, а не выводится из ширины головки: головка ставит
// грань на место, но подошва шире её вдвое, и тело, отмеренное головкой, было бы
// вдвое ýже настоящего — а вопрос о смычке решается именно телом.
//
// Сечения нет — работает ОБЪЯВЛЕННОЕ упрощение контракта: прямоугольник
// head_width от грани наружу. То же умолчание, что у клиента, и названо оно тем
// же словом: не подстановка, а объявленный прямоугольник.
func railSpan(r RenderRail) (near, far float64) {
	if len(r.Section) == 0 {
		return 0, r.HeadWidth
	}
	near, far = r.Section[0][0], r.Section[0][0]
	for _, pt := range r.Section {
		near = math.Min(near, pt[0])
		far = math.Max(far, pt[0])
	}
	return near, far
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

// bladeScaleAt — доля полного сечения остряка на расстоянии u от острия.
//
// Читается ТА ЖЕ таблица, что уехала на провод (RenderTurnoutBlade.Section), и
// теми же двумя правилами — линейно между станциями, постоянно за последней.
// Второго чтения строжки проект не заводит: разойдясь, они дали бы проверку,
// доказывающую смычку у формы, которой клиент не строит.
//
// Пустая таблица даёт единицу — полное сечение. Это не подстановка: остряк без
// строжки клиентом не строится вовсе (track_build.gd), и сборка такого
// устройства проверяет то, чего никто не покажет. Отказ здесь называл бы
// дефектом отсутствие детали, а её отсутствие — вопрос другой проверки.
func bladeScaleAt(section []RenderSectionStation, u, fullFar float64) float64 {
	if len(section) == 0 || fullFar <= 0 {
		return 1
	}
	head := section[len(section)-1].HeadWidth
	if head <= 0 {
		return 1
	}
	w := section[len(section)-1].HeadWidth
	switch {
	case u <= section[0].U:
		w = section[0].HeadWidth
	case u >= section[len(section)-1].U:
		// За последней станцией постоянно: остряк бывает длиннее таблицы.
	default:
		for i := 0; i+1 < len(section); i++ {
			lo, hi := section[i], section[i+1]
			if u > hi.U {
				continue
			}
			span := hi.U - lo.U
			if span <= 0 {
				w = lo.HeadWidth
				break
			}
			t := (u - lo.U) / span
			w = lo.HeadWidth + (hi.HeadWidth-lo.HeadWidth)*t
			break
		}
	}
	return w / head
}

// flaredParts раскладывает отогнутую нитку на звенья ПОСТОЯННОГО ЗАКОНА ГРАНИ.
//
// # Зачем это понадобилось, и чем обошлась прежняя запись
//
// Усовик — не рейка рядом с ниткой, а САМА нитка, отведённая наружу перед
// сердечником и возвращающаяся за ним (frograils.go): концы садятся ровно на
// нитку (EndFace), середина отстоит на ширину желоба (Face), между ними отгиб.
// Клиент читает ровно этот кусочно-линейный закон (track_build.gd::_flared).
//
// Первая редакция сборки записывала усовик ОДНОЙ деталью с FaceFrom = Face и
// FaceTo = EndFace, то есть ставила грань отогнутой уже в начале. Проверка
// сравнивала форму, которой клиент не строит, — нарушая инвариант, объявленный
// в шапке этого файла. Цена: четыре ложных несмыкания по 69 %, и число выдаёт
// причину точно — (0.150 − 0.046) / 0.150 = 69.3 %, где 0.046 есть ширина
// желоба, а 0.150 — ширина сечения.
//
// Хуже цены сам класс ошибки: я собирался ОБЪЯВИТЬ эти 69 % законным желобом.
// Объявление узаконило бы дефект сборщика — ровно то, от чего модель уводит.
// Настоящий желоб лежит ВНУТРИ усовика, а не на его порту, и объявлять на порту
// было нечего.
//
// Звеньев три, а не одно с законом сечения: Part описывает грань линейно, и три
// линейных звена выражают кусочно-линейный закон точно. Заводить закон грани
// станциями стоило бы дороже и понадобится не здесь, а на переводной кривой.
func flaredParts(id, kind, owner string, r RenderTurnoutRail, near, far, head float64) []Part {
	seg := func(suffix string, from, to, faceFrom, faceTo float64) Part {
		return Part{
			ID: id + suffix, Kind: kind, Owner: owner, Element: r.Element,
			FromU: from, ToU: to, FaceFrom: faceFrom, FaceTo: faceTo,
			Grow: r.Grow, Near: near, Far: far, Head: head, ScaleFrom: 1, ScaleTo: 1,
		}
	}
	length := r.To - r.From
	if r.Flare <= 0 || length <= 0 {
		// Отгиба нет — нитка прямая во всю длину. Ветка не про вырожденные данные,
		// а про законный случай: контррельс без отгиба остаётся контррельсом.
		return []Part{seg("", r.From, r.To, r.Face, r.Face)}
	}
	if 2*r.Flare >= length {
		// ОТГИБЫ СОШЛИСЬ, не дав рабочей части. Полного выноса нитка не набирает,
		// и звеньев два с общей серединой — то же, что даст клиент: у него d
		// считается от БЛИЖНЕГО конца, и в середине короткой нитки отгиб просто не
		// доходит до конца.
		mid := (r.From + r.To) / 2
		peak := r.EndFace + (r.Face-r.EndFace)*(length/2)/r.Flare
		return []Part{
			seg("|in", r.From, mid, r.EndFace, peak),
			seg("|out", mid, r.To, peak, r.EndFace),
		}
	}
	return []Part{
		seg("|in", r.From, r.From+r.Flare, r.EndFace, r.Face),
		seg("|body", r.From+r.Flare, r.To-r.Flare, r.Face, r.Face),
		seg("|out", r.To-r.Flare, r.To, r.Face, r.EndFace),
	}
}
