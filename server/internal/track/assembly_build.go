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
// РАЗРЫВЫ БЕРУТСЯ У ТОГО, КТО ИХ ОБЪЯВИЛ, и сегодня их не объявляет никто.
//
// Один прожил здесь ровно день: «наружной нитки бокового маршрута в пределах
// стрелки нет НАРОЧНО, её работу несёт рамный рельс прямого пути». Замер его
// опроверг, и вместе с ним ушла причина — раскладка рамных рельсов оказалась
// неверной (turnout_running.go). Урок стоит того, чтобы стоять здесь: ОБЪЯВЛЕННЫЙ
// РАЗРЫВ ГАСИТ ПРОВЕРКУ (coveredByGap), и неверное объявление слепит её ровно на
// том месте, ради которого она заведена.
//
// Разрывы по-прежнему не ВЫВОДЯТСЯ здесь и не подставляются: сборка берёт ровно
// то, что объявлено, и всё прочее считает несмыканием.
func AssembleTurnout(rg *RenderGeometry, els map[string]Element, owner string) (Assembly, error) {
	a := Assembly{Owner: owner}
	for _, g := range rg.RailGaps {
		if g.Owner != owner {
			continue
		}
		a.Gaps = append(a.Gaps, Gap{
			Kind: g.Kind, Element: g.Element, Face: g.Face,
			From: g.From, To: g.To, Why: g.Why,
		})
	}

	tt, err := turnoutTrackType(rg, owner)
	if err != nil {
		return Assembly{}, err
	}
	if tt.Gauge <= 0 {
		return Assembly{}, fmt.Errorf("track: сборка %s: у типа %s колея %v", owner, tt.ID, tt.Gauge)
	}
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
			// СТОРОНА БЕРЁТСЯ У ТЕЛА (BodyGrow), А НЕ У ПОРТА (Grow), и это тот же
			// инвариант, что в шапке файла: проверять надо то, что уедет на провод.
			//
			// Сегодня они равны (blade.go), и разницы в числах нет. Но пока сборка
			// читала Grow, она доказывала смычку у тела, КОТОРОГО КЛИЕНТ НЕ СТРОИЛ:
			// клиент кладёт остряк по body_grow, и в корне кривого остряка металл
			// прыгал на 58 мм при зелёной проверке в Go. Разойдись поля снова —
			// увидит это сборка, а не кадр.
			Grow: b.BodyGrow,
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

	for i, r := range rg.Rails {
		if r.Owner != owner {
			continue
		}
		kind := PartWingRail
		switch r.Kind {
		case TurnoutRailStock:
			kind = PartStockRail
		case TurnoutRailClosure:
			kind = PartRail
		case FrogRailCheck:
			kind = PartCheckRail
		case FrogRailCasting:
			kind = PartFrogCasting
		}
		id := r.ID
		if id == "" {
			id = fmt.Sprintf("%s|%s|%d", r.Element, kind, i)
		}
		a.Parts = append(a.Parts, flaredParts(id, kind, owner, r, near, far, head)...)
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
// нитку (EndFaceFrom/EndFaceTo), середина отстоит на ширину желоба (Face), между
// ними отгиб.
// Клиент читает ровно этот кусочно-линейный закон (track_build.gd::_flared).
//
// Первая редакция сборки записывала усовик ОДНОЙ деталью с FaceFrom = Face и
// FaceTo = гранью конца, то есть ставила грань отогнутой уже в начале. Проверка
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
func flaredParts(id, kind, owner string, r RenderRailPart, near, far, head float64) []Part {
	seg := func(suffix string, from, to, faceFrom, faceTo float64) Part {
		return Part{
			ID: id + suffix, Kind: kind, Owner: owner, Element: r.Element,
			FromU: from, ToU: to, FaceFrom: faceFrom, FaceTo: faceTo,
			Grow: r.Grow, Near: near, Far: far, Head: head, ScaleFrom: 1, ScaleTo: 1,
		}
	}
	length := r.To - r.From
	if length <= 0 {
		return nil
	}
	// ОТГИБЫ У КОНЦОВ РАЗНЫЕ, и потому звенья считаются порознь. Раньше здесь
	// стояла одна длина на оба конца и три случая («нет отгиба», «отгибы сошлись»,
	// «полный»); у усовика, который у горла переходит на соседнюю нитку, отгиб
	// есть только с одного конца, и симметричная раскладка ставила ему второй —
	// то есть уводила грань с выноса ровно в той точке, где она обязана быть на
	// выносе (разбор — в frograils.go).
	in, out := r.FlareFrom, r.FlareTo
	if in <= 0 && out <= 0 {
		// Отгибов нет — нитка прямая во всю длину. Ветка не про вырожденные данные,
		// а про законный случай: рамный рельс без отгиба остаётся рамным рельсом.
		return []Part{seg("", r.From, r.To, r.Face, r.Face)}
	}
	if sum := in + out; sum > length {
		// ОТГИБЫ СОШЛИСЬ, не дав рабочей части: полного выноса нитка не набирает.
		// Прижимаются оба в одной пропорции — то же, что делает построитель детали
		// (frograils.clampFlares), и то же, что даст клиент.
		k := length / sum
		in *= k
		out *= k
	}
	parts := make([]Part, 0, 3)
	at := r.From
	if in > 0 {
		parts = append(parts, seg("|in", at, at+in, r.EndFaceFrom, r.Face))
		at += in
	}
	if body := r.To - out; body > at {
		parts = append(parts, seg("|body", at, body, r.Face, r.Face))
		at = body
	}
	if out > 0 {
		parts = append(parts, seg("|out", at, r.To, r.Face, r.EndFaceTo))
	}
	return parts
}
