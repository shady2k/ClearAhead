package content

// extent.go — ГАБАРИТ СОБРАННОГО ТЕЛА, посчитанный в Go по описанию.
//
// # Зачем это здесь, а не у клиента, который тело и рисует
//
// Клиент габарит знает — стенд его печатает, — но знает ПОСЛЕ сборки сцены, то
// есть глазами человека, который догадался посмотреть. Ровно так и вышло
// 2026-08-18: тело полувагона врало в пяти размерах (кузов длиннее рамы на
// 1.14 м, борт вдвое толще, пол и верх борта не на своих отметках, автосцепки
// утоплены внутрь рамы), проверки при этом были зелёными, и поймал это владелец
// на кадре. Тот же класс, что и замороженная сеть неделей раньше: если
// единственный сторож — глаз, сторожа нет.
//
// Здесь считается ТО ЖЕ ЧИСЛО, что показывает стенд, но без Godot и до того,
// как набор вообще соберётся: паспорт называет длину, ширину и высоту машины,
// тело обязано этими числами и ограничиваться. Расхождение — отказ загрузки.
//
// # Чего этот счёт НЕ доказывает
//
// Внутренних размеров. Кузов, оказавшийся длиннее рамы, в габарит вписывается
// и здесь не ловится — его ловит только источник, в котором написано «12.78 м
// по концевым балкам». Проверка сторожит ВНЕШНЮЮ границу, и это ровно половина
// вопроса; вторая половина покупается числами, а не кодом.

import (
	"fmt"
	"math"
)

// Box — габарит тела в осях модели: x поперёк, y вверх от поверхности катания,
// z вдоль. Пустой (пришедший из тела без единой части) отличим от нулевого:
// Empty говорит это прямо, потому что нули по всем шести граням выглядят как
// точка в начале координат.
type Box struct {
	MinX, MaxX float64
	MinY, MaxY float64
	MinZ, MaxZ float64
	Empty      bool
}

// Width, Height, Length — три числа, которыми машину называет паспорт.
//
// Высота меряется ОТ ПОВЕРХНОСТИ КАТАНИЯ, а не от низа тела: датум вертикали в
// проекте один (контракт отрисовки редакции 6 §2), и колесо, уходящее ниже
// головки рельса, высоту машины не увеличивает.
func (b Box) Width() float64  { return b.MaxX - b.MinX }
func (b Box) Height() float64 { return b.MaxY }
func (b Box) Length() float64 { return b.MaxZ - b.MinZ }

func (b *Box) add(minX, maxX, minY, maxY, minZ, maxZ float64) {
	if b.Empty {
		b.MinX, b.MaxX = minX, maxX
		b.MinY, b.MaxY = minY, maxY
		b.MinZ, b.MaxZ = minZ, maxZ
		b.Empty = false
		return
	}
	b.MinX = math.Min(b.MinX, minX)
	b.MaxX = math.Max(b.MaxX, maxX)
	b.MinY = math.Min(b.MinY, minY)
	b.MaxY = math.Max(b.MaxY, maxY)
	b.MinZ = math.Min(b.MinZ, minZ)
	b.MaxZ = math.Max(b.MaxZ, maxZ)
}

// Extent — габарит тела при заданных величинах экземпляра.
//
// ПОВОРОТ ЧАСТИ УЧИТЫВАЕТСЯ ТОЧНО, а не запасом: восемь вершин ящика
// поворачиваются и обмеряются заново. Запас («расширить на диагональ») дал бы
// габарит больше настоящего, и проверка отвергала бы верное тело — то есть
// приучала бы к обходу.
//
// ПОДВИЖНОСТЬ (Pivot) считается в НУЛЕВОМ положении: габарит машины меряют у
// стоящей машины, а не у той, у которой открыта дверь. Растяжимость (Stretch)
// берётся авторским размером по той же причине — величину ей задаёт мир, и до
// мира её здесь нет.
func (m *Model) Extent(params map[string]float64) (Box, error) {
	b := Box{Empty: true}
	for i := range m.Parts {
		if err := extendBox(&b, m.Parts[i], params, 0, 0, 0); err != nil {
			return Box{}, fmt.Errorf("тело %s: %w", m.Name, err)
		}
	}
	return b, nil
}

// extendBox добавляет к габариту часть и её детей. Смещения складываются: у
// вложенной части координата отсчитывается от родителя, а не от начала тела.
func extendBox(b *Box, p Part, params map[string]float64, ox, oy, oz float64) error {
	at, err := resolveTriple(p.At, params)
	if err != nil {
		return fmt.Errorf("часть %s: at: %w", p.Name, err)
	}
	cx, cy, cz := ox+at[0], oy+at[1], oz+at[2]

	half, err := halfSizeOf(p, params)
	if err != nil {
		return fmt.Errorf("часть %s: %w", p.Name, err)
	}
	if half != nil {
		addRotated(b, cx, cy, cz, half[0], half[1], half[2], p.Rotate)
	}
	for i := range p.Parts {
		if err := extendBox(b, p.Parts[i], params, cx, cy, cz); err != nil {
			return err
		}
	}
	return nil
}

// halfSizeOf — половина габарита части в её собственных осях. nil у части без
// формы: группа сама по себе места не занимает, его занимают её дети.
func halfSizeOf(p Part, params map[string]float64) (*[3]float64, error) {
	switch p.Shape {
	case ShapeGroup: // пустая строка — она же (ShapeGroup = "")
		return nil, nil
	case ShapeBox:
		s, err := resolveTriple([3]Value{p.Size[0], p.Size[1], p.Size[2]}, params)
		if err != nil {
			return nil, err
		}
		return &[3]float64{s[0] / 2, s[1] / 2, s[2] / 2}, nil
	case ShapeCylinder, ShapeFrustum:
		var r float64
		if p.Shape == ShapeCylinder {
			v, err := p.Radius.Resolve(params)
			if err != nil {
				return nil, err
			}
			r = v
		} else {
			// У конуса шире тот конец, который больше: габарит берёт больший.
			bot, err := p.Bottom.Resolve(params)
			if err != nil {
				return nil, err
			}
			top, err := p.Top.Resolve(params)
			if err != nil {
				return nil, err
			}
			r = math.Max(bot, top)
		}
		h, err := p.Height.Resolve(params)
		if err != nil {
			return nil, err
		}
		out := [3]float64{r, r, r}
		switch p.Axis {
		case "x":
			out[0] = h / 2
		case "y":
			out[1] = h / 2
		default:
			out[2] = h / 2
		}
		return &out, nil
	case ShapePlate:
		s, err := resolveTriple([3]Value{p.Size[0], p.Size[1], p.Thickness}, params)
		if err != nil {
			return nil, err
		}
		// Щиток лежит в плоскости своих осей: третий размер — толщина.
		return &[3]float64{s[0] / 2, s[1] / 2, s[2] / 2}, nil
	}
	return nil, fmt.Errorf("форма %q габарита не имеет", p.Shape)
}

// addRotated обмеряет повёрнутый ящик по его восьми вершинам.
func addRotated(b *Box, cx, cy, cz, hx, hy, hz float64, rot [3]float64) {
	if rot[0] == 0 && rot[1] == 0 && rot[2] == 0 {
		b.add(cx-hx, cx+hx, cy-hy, cy+hy, cz-hz, cz+hz)
		return
	}
	// Формат допускает поворот ВОКРУГ ОДНОЙ ОСИ (Part.Rotate), и порядок Эйлера
	// поэтому безразличен — здесь применяются все три подряд честно, а не
	// выбирается одна: часть с двумя осями до сюда не доходит (валидатор её
	// отвергает), и если однажды дойдёт, счёт останется верным.
	sx, cxr := math.Sincos(rot[0] * math.Pi / 180)
	sy, cyr := math.Sincos(rot[1] * math.Pi / 180)
	sz, czr := math.Sincos(rot[2] * math.Pi / 180)
	for i := range 8 {
		x := hx
		if i&1 != 0 {
			x = -x
		}
		y := hy
		if i&2 != 0 {
			y = -y
		}
		z := hz
		if i&4 != 0 {
			z = -z
		}
		// X
		y, z = y*cxr-z*sx, y*sx+z*cxr
		// Y
		x, z = x*cyr+z*sy, -x*sy+z*cyr
		// Z
		x, y = x*czr-y*sz, x*sz+y*czr
		b.add(cx+x, cx+x, cy+y, cy+y, cz+z, cz+z)
	}
}

func resolveTriple(v [3]Value, params map[string]float64) ([3]float64, error) {
	var out [3]float64
	for i := range v {
		r, err := v[i].Resolve(params)
		if err != nil {
			return out, err
		}
		out[i] = r
	}
	return out, nil
}

// ГАБАРИТ ПОДВИЖНОГО СОСТАВА 1-Т (ГОСТ 9238-2013): предельные очертания, в
// которые обязана вписываться машина, допущенная к обращению по всей сети.
//
// Числа источника, а не наши: ширина до 3400 мм, высота над головкой рельса до
// 5300 мм. Есть и увеличенный габарит Т (ширина до 3580…3750), но он для
// отдельных видов состава на специально подготовленных путях — набор, которому
// понадобится Т, назовёт его сам, и тогда предел станет полем паспорта, а не
// одной константой на всех.
const (
	GaugeTWidthM  = 3.4
	GaugeTHeightM = 5.3
)

// bodyToleranceM — на сколько собранное тело вправе разойтись с паспортом.
//
// Пять миллиметров, и это не «примерно»: паспорт объявляет габарит в
// сантиметрах реальной машины (3.158 м ширины), а тело складывается из литералов
// того же порядка. Расхождение в полсантиметра означает описку в размере детали,
// а не накопленную ошибку сложения — двойная точность на пятидесяти частях даёт
// разницу порядка 1e-15.
const bodyToleranceM = 0.005

// checkStockBodies — тело подвижной единицы обязано СХОДИТЬСЯ С ПАСПОРТОМ.
//
// # Почему равенство, а не «влезает»
//
// Потому что паспорт объявляет ГАБАРИТ — предельные внешние очертания машины, а
// не разрешение занять меньше. Тело уже машины означает, что паспорт врёт про
// её ширину; тело шире — что машина не вписывается в собственный габарит и
// заденет платформу. Оба случая — отказ, и отличить их друг от друга обязан
// текст отказа, а не читатель.
//
// # Что этим ловится, а что нет
//
// Ловится: деталь, вылезшая за габарит (стойка борта снаружи обшивки), тело,
// собранное не по тем числам, паспорт, переписанный без тела. Не ловится:
// внутренние размеры — кузов, оказавшийся длиннее рамы, лежит внутри габарита и
// виден только источнику. Это записано и в шапке файла: проверка сторожит
// внешнюю границу, вторая половина покупается числами.
func (s *Set) checkStockBodies() error {
	for _, t := range s.Stock {
		a, ok := s.asset(t.Appearance)
		if !ok || a.MediaType != ModelMediaType {
			// Вид запечён чужим glTF: обмерять его здесь нечем, и это
			// названная граница, а не пропуск. Постановку такого вида
			// сторожит зонд по доехавшему мешу (client/tools/stock_probe.gd).
			continue
		}
		m, ok := s.models[a.Name]
		if !ok {
			return fmt.Errorf("content: тип %s: тело %s не разобрано", t.ID, a.Name)
		}
		box, err := m.Extent(map[string]float64{
			"length":     t.LengthM,
			"width":      t.WidthM,
			"height":     t.HeightM,
			"bogie_base": t.BogieBaseM,
		})
		if err != nil {
			return fmt.Errorf("content: тип %s: %w", t.ID, err)
		}
		if box.Empty {
			return fmt.Errorf("content: тип %s: тело %s не содержит ни одной части", t.ID, a.Name)
		}
		for _, d := range []struct {
			name      string
			got, want float64
		}{
			{"длина", box.Length(), t.LengthM},
			{"ширина", box.Width(), t.WidthM},
			{"высота над поверхностью катания", box.Height(), t.HeightM},
		} {
			if math.Abs(d.got-d.want) > bodyToleranceM {
				return fmt.Errorf("content: тип %s: у собранного тела %s %s %.3f м, "+
					"паспорт объявляет %.3f м (расхождение %.3f м при допуске %.3f)",
					t.ID, a.Name, d.name, d.got, d.want, d.got-d.want, bodyToleranceM)
			}
		}
		// НИЖЕ ГОЛОВКИ РЕЛЬСА ТЕЛО НЕ ОПУСКАЕТСЯ БОЛЬШЕ, ЧЕМ НА КОЛЕСО. Датум
		// вертикали — поверхность катания, и деталь под ней означает либо
		// провалившийся кузов, либо перепутанный знак: колесо касается рельса
		// снизу ровно в нуле, а всё остальное выше.
		if box.MinY < -bodyToleranceM {
			return fmt.Errorf("content: тип %s: тело %s уходит на %.3f м НИЖЕ поверхности катания",
				t.ID, a.Name, -box.MinY)
		}
	}
	return nil
}

// CheckLoadingGauge — вписывается ли КАЖДЫЙ паспорт набора в габарит 1-Т.
//
// # Как она включалась
//
// Написана и НЕ включена в тот же день, 2026-08-18: первым её нарушал наш
// собственный ВЛ80 — 3.63 × 5.40 м, числа не машины, а замер чужого меша (бида
// ClearAhead-w4q, где это предсказано ещё 2026-08-12). Выбор был между
// «подставить локомотиву правдоподобные числа» — то есть сделать ровно то, за
// что проверка и заведена, — и «оставить её невключённой, пока не появится
// источник». Выбрано второе: проверка, ради зелени которой подделали данные,
// хуже отсутствующей.
//
// Источник появился через час: 32.84 × 3.24 × 5.10 м, и проверка включена в
// Load. Час — это цена вопроса «а откуда число», заданного вслух вместо
// подстановки правдоподобного.
func (s *Set) CheckLoadingGauge() error {
	for _, t := range s.Stock {
		if t.WidthM > GaugeTWidthM || t.HeightM > GaugeTHeightM {
			return fmt.Errorf("content: тип %s: %.3f × %.3f м не вписывается в габарит 1-Т "+
				"(%.1f × %.1f м, ГОСТ 9238-2013); машина такого очертания по сети не обращается",
				t.ID, t.WidthM, t.HeightM, GaugeTWidthM, GaugeTHeightM)
		}
	}
	return nil
}
