package track

import (
	"fmt"

	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// ОТРЕЗОК ПУТИ, ЗАНЯТЫЙ ОДНИМ ТЕЛОМ.
//
// # Что он лечит и почему без него было нельзя
//
// До 2026-08-15 машина адресовалась ТОЧКОЙ, а протяжённость выводилась из неё
// полудлиной в обе стороны. Пока тело помещается в один элемент, это верно; на
// границе — нет, и цена платилась в трёх местах сразу:
//
//	УПОР      ловил конец машины через полдлины от точки отсчёта. У длинного
//	          состава конец окажется не там (объявлено в sim.move).
//	ХОРДА     клиент ставит машину серединой хорды между шкворнями, а позы за
//	          концом элемента не существует — шкворень прижимался к границе, и
//	          лечением было СЖАТИЕ хорды у конца (ClearAhead-cc0x). Цена сжатия:
//	          у конца элемента поправка пропадает.
//	ПОКАЗ     правило «через границу элемента не интерполируем» отдаёт на
//	          переходе целый промежуток снимков разом. ЗАМЕР: 0.72…0.73 м за кадр
//	          на каждой смене элемента при 6.9 м/с.
//
// Все три — одно и то же незнание: где лежат КОНЦЫ тела. Отрезок его снимает.
//
// # Порядок визитов: от конца B к концу A
//
// У машины два конца, и они названы не «передний» и «задний»: перёд зависит от
// реверсора, а концы — нет. Конец A — тот, на который смотрит направление
// единицы (netloc.Point.Direction: DirForward значит «A смотрит в сторону роста
// u»). Порядок визитов от B к A ПОСТОЯНЕН: он не переворачивается ни при смене
// хода, ни при въезде на встречный элемент, — иначе всякий, кто держит ссылку
// на визит, держал бы её на то, что молча стало другим концом.
//
// Направление визита говорит ровно одно: совпадает ли ход B → A с ростом u
// ЭТОГО элемента. Оси соседних элементов бывают встречны, поэтому направление
// живёт у каждого визита, а не одно на весь отрезок.
//
// # Пара портов НЕ хранится, и это не пропуск
//
// Бида просила «SegmentVisit с фактически пройденной парой портов». Пара портов
// у визита ЕСТЬ (см. VisitPorts), но она ВЫВОДИТСЯ из элемента и направления: у
// элемента ровно два порта, и направление говорит, каким из них вошли. Хранить
// её рядом значило бы завести второе написание одной величины — то самое, за
// что проект отвергает интервал у подвижной единицы (netloc.Point). Правило
// сильнее любой проверки, которая его сторожит.
//
// ЧТО ИМЕННО ХРАНИТСЯ КАК ИСТОРИЯ — сама цепочка визитов. Она записана в тот
// миг, когда ведущий конец пересёк границу, и переставленный после этого остряк
// её не меняет. Это и есть «фактически пройденное»: спросить сеть заново
// значило бы получить путь, по которому тело НЕ ехало.
//
// # Полуоткрытые интервалы
//
// Наложение считается по [from, to): касание концами наложением НЕ является.
// Соглашение принято раньше и не здесь (ClearAhead-5zd) — при целых микрометрах
// равенство достижимо, и «какая из двух сторон границы занята» не имеет
// естественного ответа.
type Span netloc.LinearS

// MaxVisits — потолок числа визитов в одном отрезке.
//
// Стоит против бесконечного круга по замкнутой петле нулевой длины, то есть
// против поломки карты, а не против нормы: у самого длинного мыслимого состава
// на самой мелко нарезанной горловине визитов десятки, не тысячи. Число то же
// по смыслу, что прежний потолок переходов за шаг в sim.move (шестнадцать), но
// больше: там считались переходы ОДНОЙ точки за один шаг, здесь — элементы под
// всем телом сразу.
const MaxVisits = 256

// sideB — координата конца B у визита: та, с которой ход B → A начинается.
func sideB(iv netloc.IntervalS) units.Distance {
	if iv.Direction == netloc.DirForward {
		return iv.From
	}
	return iv.To
}

// sideA — координата конца A у визита.
func sideA(iv netloc.IntervalS) units.Distance {
	if iv.Direction == netloc.DirForward {
		return iv.To
	}
	return iv.From
}

// Length — длина отрезка. У поставленного тела равна его длине по паспорту, и
// это ИНВАРИАНТ, а не совпадение: наращивание и усечение идут на одну и ту же
// величину (см. sim.move).
func (s Span) Length() units.Distance {
	var total units.Distance
	for _, iv := range s {
		total += iv.To - iv.From
	}
	return total
}

// Structural — проверка, не требующая сети: непустота, форма интервала,
// заданное направление у каждого визита.
//
// Направление ОБЯЗАТЕЛЬНО, в отличие от netloc.Interval вообще: пустое значение
// там означает «объект направления не имеет», и это правда для платформы. У
// визита тела оно означало бы, что неизвестно, каким концом тело лежит на
// элементе, — то есть что отрезок недоописан.
func (s Span) Structural() error {
	if len(s) == 0 {
		return fmt.Errorf("track: пустой отрезок пути")
	}
	if len(s) > MaxVisits {
		return fmt.Errorf("track: в отрезке %d визитов, потолок %d", len(s), MaxVisits)
	}
	for i, iv := range s {
		if err := iv.Structural(); err != nil {
			return fmt.Errorf("track: визит %d: %w", i, err)
		}
		if !iv.Direction.Directed() {
			return fmt.Errorf("track: визит %d (элемент %s): направление не задано; "+
				"у визита тела оно означает, каким концом тело лежит на элементе",
				i, iv.Element)
		}
	}
	return nil
}

// Connected — полная проверка отрезка по сети.
//
// Проверяется то, что Structural проверить не может, потому что не знает ни
// длин элементов, ни связности:
//
//	каждый визит лежит в пределах своего элемента;
//	промежуточные визиты покрывают элемент ЦЕЛИКОМ — тело, прошедшее элемент
//	  насквозь, занимает его весь, и дыра означала бы разорванное тело;
//	соседние визиты сходятся в ОДНОМ порту, и сходятся теми концами, которыми
//	  тело через них шло.
//
// Зовётся не в горячем пути: наращивание строит отрезок правильным по
// построению, а это — сторож для тестов и для загрузки, где отрезок приходит
// извне.
func (s Span) Connected(net *CompiledNetwork) error {
	if err := s.Structural(); err != nil {
		return err
	}
	for i, iv := range s {
		el, ok := net.Elements[iv.Element]
		if !ok {
			return fmt.Errorf("track: визит %d: элемента %s нет в сети", i, iv.Element)
		}
		if iv.From < 0 || iv.To > el.LengthS {
			return fmt.Errorf("track: визит %d: [%s, %s] вне элемента %s длиной %s",
				i, iv.From, iv.To, iv.Element, el.LengthS)
		}
		// Промежуточный визит обязан быть сквозным: тело зашло в элемент одним
		// портом и вышло другим, значит заняло его целиком.
		if i > 0 && sideB(iv) != endOf(el, iv.Direction, false) {
			return fmt.Errorf("track: визит %d (элемент %s): конец B не в порту входа", i, iv.Element)
		}
		if i < len(s)-1 && sideA(iv) != endOf(el, iv.Direction, true) {
			return fmt.Errorf("track: визит %d (элемент %s): конец A не в порту выхода", i, iv.Element)
		}
		if i == 0 {
			continue
		}
		prev, ok := net.Elements[s[i-1].Element]
		if !ok {
			return fmt.Errorf("track: визит %d: элемента %s нет в сети", i-1, s[i-1].Element)
		}
		_, exit := VisitPorts(s[i-1], prev)
		entry, _ := VisitPorts(iv, el)
		if exit != entry {
			return fmt.Errorf("track: визиты %d и %d не сходятся: выход %s, вход %s",
				i-1, i, exit, entry)
		}
	}
	return nil
}

// endOf — координата того конца элемента, которым визит смотрит в сторону A
// (toA) либо в сторону B.
func endOf(el CompiledElement, dir netloc.Direction, toA bool) units.Distance {
	if (dir == netloc.DirForward) == toA {
		return el.LengthS
	}
	return 0
}

// VisitPorts — пара портов, которой тело через элемент ПРОШЛО: вход со стороны
// конца B, выход со стороны конца A.
//
// Выводится, а не хранится: разбор — в шапке типа Span.
//
// ФУНКЦИЕЙ, А НЕ МЕТОДОМ, и не по вкусу: визит — это netloc.IntervalS, то есть
// тип чужого пакета, и метода на нём не объявить. Заводить ради метода свой тип
// значило бы завести шестую копию формы протяжённости — ровно то, ради отмены
// чего пакет netloc и появился.
func VisitPorts(iv netloc.IntervalS, el CompiledElement) (entry, exit string) {
	if iv.Direction == netloc.DirForward {
		return el.From, el.To
	}
	return el.To, el.From
}

// Reverse — тот же отрезок, прочитанный с другого конца.
//
// # Зачем это нужно на самом деле
//
// Не ради симметрии. Наращивание и усечение написаны ОДИН раз — для конца A, —
// а конец B обслуживается разворотом: GrowB(d) это Reverse().GrowA(d).Reverse().
// Поэтому реверс не украшение, а несущая конструкция, и его свойства
// (двукратный реверс — тождество, длина не меняется, связность сохраняется)
// проверяются property-тестами.
//
// Разворачивается ПОРЯДОК и НАПРАВЛЕНИЕ каждого визита; координаты интервала не
// трогаются вовсе — они живут в оси элемента, а не в оси тела, и от того, с
// какого конца тело читают, не зависят (netloc.Interval: From < To всегда).
func (s Span) Reverse() Span {
	out := make(Span, len(s))
	for i, iv := range s {
		iv.Direction = flip(iv.Direction)
		out[len(s)-1-i] = iv
	}
	return out
}

func flip(d netloc.Direction) netloc.Direction {
	switch d {
	case netloc.DirForward:
		return netloc.DirReverse
	case netloc.DirReverse:
		return netloc.DirForward
	}
	return d
}

// PointAt — точка на расстоянии d от конца B вдоль отрезка.
//
// Отдаёт элемент, координату s на нём и направление визита, в который попала:
// последнее и есть ответ на вопрос «как тело повёрнуто относительно роста u
// ЗДЕСЬ», то есть netloc.Direction точки отсчёта.
//
// Граница визитов принадлежит БОЛЕЕ РАННЕМУ визиту (ближнему к B), кроме
// нулевого расстояния: иначе точка ровно на стыке отвечала бы двумя разными
// элементами в зависимости от того, с какой стороны её спросили.
func (s Span) PointAt(d units.Distance) (element string, at units.Distance, dir netloc.Direction, ok bool) {
	if len(s) == 0 || d < 0 {
		return "", 0, "", false
	}
	rest := d
	for _, iv := range s {
		span := iv.To - iv.From
		if rest > span {
			rest -= span
			continue
		}
		if iv.Direction == netloc.DirForward {
			return iv.Element, iv.From + rest, iv.Direction, true
		}
		return iv.Element, iv.To - rest, iv.Direction, true
	}
	return "", 0, "", false
}

// Middle — точка отсчёта тела: середина отрезка.
//
// Определение точки отсчёта одно на весь проект — середина между плоскостями
// автосцепок (match.Unit.At), — и здесь оно применяется буквально: длина
// отрезка равна длине тела, значит его середина и есть точка отсчёта.
func (s Span) Middle() (element string, at units.Distance, dir netloc.Direction, ok bool) {
	return s.PointAt(s.Length() / 2)
}

// Overlaps — накладывается ли отрезок на другой.
//
// Отдаёт элемент и наложившийся интервал: сообщение об отказе обязано называть
// МЕСТО, а не факт. Полуоткрытые интервалы — разбор в шапке типа.
func (s Span) Overlaps(other Span) (element string, at netloc.IntervalS, ok bool) {
	for _, a := range s {
		for _, b := range other {
			if a.Element != b.Element {
				continue
			}
			if a.From < b.To && b.From < a.To {
				return a.Element, netloc.IntervalS{
					Element: a.Element,
					From:    max(a.From, b.From),
					To:      min(a.To, b.To),
				}, true
			}
		}
	}
	return "", netloc.IntervalS{}, false
}

// GrowA — нарастить отрезок с конца A на d.
//
// Возвращает наращённый отрезок и НЕПРОЙДЕННЫЙ ОСТАТОК. Остаток не ноль ровно в
// одном случае — дальше пути нет: тупик, край карты либо стрелка, стоящая не по
// нашему ходу. Это и есть упор, и теперь в него встаёт настоящий КОНЕЦ тела, а
// не полдлины от точки отсчёта: длинный состав встанет там же, где короткий,
// потому что упирается тем, чем упираются на самом деле.
//
// Ошибка означает поломку мира (элемента нет в сети, петля нулевой длины), а не
// отказ: испорченную сеть ловят при компиляции, и здесь она уже невозможна.
func (s Span) GrowA(w Walk, d units.Distance) (Span, units.Distance, error) {
	if d < 0 {
		return nil, 0, fmt.Errorf("track: наращивание на отрицательное %s", d)
	}
	if len(s) == 0 {
		return nil, 0, fmt.Errorf("track: наращивание пустого отрезка")
	}
	out := append(Span(nil), s...)
	for d > 0 {
		last := &out[len(out)-1]
		el, ok := w.Element(last.Element)
		if !ok {
			return nil, 0, fmt.Errorf("track: элемента %s нет в сети", last.Element)
		}
		// Сколько ещё места на этом элементе в сторону A.
		var room units.Distance
		if last.Direction == netloc.DirForward {
			room = el.LengthS - last.To
		} else {
			room = last.From
		}
		if take := min(room, d); take > 0 {
			if last.Direction == netloc.DirForward {
				last.To += take
			} else {
				last.From -= take
			}
			d -= take
			if d == 0 {
				break
			}
		}
		// Конец A стоит ровно в порту. Спрашиваем сеть — и ответ ЗАПИСЫВАЕТСЯ
		// визитом: переставленный после этого остряк отрезка уже не изменит.
		_, exit := VisitPorts(*last, el)
		to, entry, ok := w.Next(el, exit)
		if !ok {
			return out, d, nil
		}
		if len(out) >= MaxVisits {
			return nil, 0, fmt.Errorf("track: отрезок разросся до %d визитов — петля в сети", len(out))
		}
		// Каким концом новый элемент прилегает к порту, тем тело в него и
		// входит: вошли началом — ход B → A совпадает с ростом u, вошли концом —
		// встречен. Знак НАЗНАЧАЕТСЯ, а не переворачивается, и это то же решение,
		// что стояло в sim.move: перевороту здесь взяться неоткуда, потому что
		// прежнего знака у нового элемента не было.
		next := netloc.IntervalS{Element: to.ID}
		if entry == 0 {
			next.Direction = netloc.DirForward
			next.From, next.To = 0, 0
		} else {
			next.Direction = netloc.DirReverse
			next.From, next.To = to.LengthS, to.LengthS
		}
		out = append(out, next)
	}
	return out, 0, nil
}

// TrimB — убрать d с конца B. Визиты, съеденные целиком, отбрасываются.
//
// Усечение больше длины отрезка — поломка вызывающего, а не край: тело не может
// стать короче себя. Отказ, а не прижатие к нулю: прижатое значение выглядело бы
// как тело нулевой длины, стоящее неизвестно где.
func (s Span) TrimB(d units.Distance) (Span, error) {
	if d < 0 {
		return nil, fmt.Errorf("track: усечение на отрицательное %s", d)
	}
	if d == 0 {
		return append(Span(nil), s...), nil
	}
	if d >= s.Length() {
		return nil, fmt.Errorf("track: усечение на %s при длине отрезка %s", d, s.Length())
	}
	out := append(Span(nil), s...)
	rest := d
	for rest > 0 {
		iv := &out[0]
		span := iv.To - iv.From
		if span > rest {
			if iv.Direction == netloc.DirForward {
				iv.From += rest
			} else {
				iv.To -= rest
			}
			break
		}
		rest -= span
		out = out[1:]
	}
	return out, nil
}

// GrowB и TrimA — то же самое с другого конца, и написаны они РАЗВОРОТОМ.
//
// Второй копии кода наращивания здесь нет нарочно: она разошлась бы с первой на
// первой же правке правил перехода, и разошлась бы в редкой ветке — при ходе
// назад. Цена разворота — два прохода по списку из единиц визитов; она названа
// и она ничтожна против цены двух разных ответов на один вопрос.
func (s Span) GrowB(w Walk, d units.Distance) (Span, units.Distance, error) {
	out, rest, err := s.Reverse().GrowA(w, d)
	if err != nil {
		return nil, 0, err
	}
	return out.Reverse(), rest, nil
}

// TrimA — убрать d с конца A.
func (s Span) TrimA(d units.Distance) (Span, error) {
	out, err := s.Reverse().TrimB(d)
	if err != nil {
		return nil, err
	}
	return out.Reverse(), nil
}

// ToU — отрезок в координатах провода.
//
// Перевод s → u делается ЗДЕСЬ и только здесь, как и у точки отсчёта
// (match.Motion.WirePoint): клиент читает ту же координату, в которой написана
// карта, и ничего не пересчитывает — правило «длины считает только Go» держится
// и у отрезка.
//
// Направление визита едет на провод как есть: клиенту оно нужно, чтобы идти по
// отрезку в ту же сторону, что и сервер, а вывести его из порядка элементов он
// не может — связности сети у него нет.
func (s Span) ToU(net *CompiledNetwork) (netloc.LinearU, error) {
	out := make(netloc.LinearU, 0, len(s))
	for i, iv := range s {
		el, ok := net.Elements[iv.Element]
		if !ok {
			return nil, fmt.Errorf("track: визит %d: элемента %s нет в сети", i, iv.Element)
		}
		from, err := el.Prof.SToU(iv.From)
		if err != nil {
			return nil, fmt.Errorf("track: визит %d: элемент %s: перевод s в u: %w", i, iv.Element, err)
		}
		to, err := el.Prof.SToU(iv.To)
		if err != nil {
			return nil, fmt.Errorf("track: визит %d: элемент %s: перевод s в u: %w", i, iv.Element, err)
		}
		out = append(out, netloc.IntervalU{
			Element:   iv.Element,
			From:      from.Meters(),
			To:        to.Meters(),
			Direction: iv.Direction,
		})
	}
	return out, nil
}

// Elements — элементы, которых касается отрезок, в порядке от B к A.
//
// Нужен обратному индексу занятости: он раскладывает отрезок по элементам, а
// повторов в отрезке не бывает — тело не может занимать один элемент дважды, не
// сделав петли, а петля означала бы состав длиннее круга.
func (s Span) Elements() []string {
	out := make([]string, 0, len(s))
	for _, iv := range s {
		out = append(out, iv.Element)
	}
	return out
}
