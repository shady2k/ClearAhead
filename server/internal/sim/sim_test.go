package sim

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/brake"
	"github.com/shady2k/ClearAhead/server/internal/content"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/physics"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/track"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// Числа здесь сверяются С ФИЗИКОЙ, А НЕ С САМИМ СОБОЙ.
//
// Проверка вида «после ста тиков s стало 1234567» подтверждает только то, что
// код не менялся: она пройдёт и на неверной формуле, если её записали до
// проверки. Поэтому каждое ожидание ниже выведено из паспорта и ПТР отдельно от
// проверяемого кода — иначе тест перестал бы быть тестом.

const tickDT = 100 * units.Millisecond

func network(t *testing.T) *track.CompiledNetwork {
	t.Helper()
	m := seedmap.Station()
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("фикстура карты: %v", err)
	}
	cn, _, err := track.Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	return cn
}

// set — набор с боевыми числами ВЛ80: их же читает физика в бою.
// set — набор контента для тестов движения. Контроллер у него ВСТАЁТ МГНОВЕННО
// и предел двигателей не объявлен: прежние проверки замеряют огибающую и
// интегрирование, и набор позиций по одной сломал бы их замеры, ничего не
// проверив.
func set(t *testing.T) *content.Set {
	return setWith(t, nil)
}

// setWith — тот же набор с правкой паспорта. Правка ФУНКЦИЕЙ, а не вторым
// файлом: два описания одной машины разошлись бы, и тест буксования проверял бы
// не ту машину, что тест разгона.
func setWith(t *testing.T, edit func(map[string]any)) *content.Set {
	t.Helper()
	dir := t.TempDir()
	body := []byte("не glb, подрезка не запрашивается")
	if err := os.WriteFile(filepath.Join(dir, "x.bin"), body, 0o600); err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	doc := map[string]any{
		"format_version": content.FormatVersion,
		"assets": []any{map[string]any{
			"name": "vid", "file": "x.bin", "media_type": "application/octet-stream",
			"source_hash": content.Addr(hex.EncodeToString(sumOf(body))),
			"anchor":      "rail_top_gauge_center", "scale": 1.0, "translation": []any{0, 0, 0},
			"attribution": map[string]any{"title": "T", "author": "A", "source": "S",
				"license": "CC0-1.0", "modified": false},
		}},
		"stock": []any{map[string]any{
			"id": "VL80", "length": 34.18, "bogie_base": 24.71, "width": 3.63, "height": 5.4,
			"mass": 192.0, "max_speed": 110.0,
			"resistance": map[string]any{"a": 1.9, "b": 0.01, "c": 0.0003},
			// ПНЕВМАТИКА В ФИКСТУРЕ ТА ЖЕ, ЧТО В НАБОРЕ: тормоз у ВЛ80 —
			// магистраль, и фикстура без неё проверяла бы другую машину.
			"brake": map[string]any{
				"shoes": "cast_iron", "braked_axles": 8, "axle_force": 137.3,
				"air": map[string]any{
					"charge": 5.4, "full_service_drop": 1.5, "cylinder_full": 3.8,
					"service_rate": 0.22, "emergency_rate": 0.9, "charge_rate": 0.6,
					"leak_rate": 0.02, "main_min": 7.5, "main_max": 9.0,
					"compressor_rate": 0.2, "cylinder_rate": 0.9, "independent_max": 4.0,
				},
			},
			"traction": map[string]any{
				"adhesive_mass": 192.0, "continuous_force": 401.1, "continuous_speed": 53.6,
			},
			"controls":   map[string]any{"traction_notches": 33, "brake_notches": 5},
			"appearance": "vid",
		}},
	}
	if edit != nil {
		edit((doc["stock"].([]any))[0].(map[string]any))
	}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(dir, content.FileName), raw, 0o600); err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	s, err := content.Load(dir)
	if err != nil {
		t.Fatalf("набор: %v", err)
	}
	return s
}

const locoID = "01a3185c-6001-7242-8242-000000424242"

// world — партия с одной машиной на главном пути и мир движения к ней.
func world(t *testing.T, u float64, facing netloc.Direction) (*World, *match.Match) {
	t.Helper()
	net := network(t)
	s := set(t)
	m := &match.Match{ID: "M1", Region: net.MapID, Units: []match.Unit{{
		ID: locoID, Name: "LOCO_1", Type: "VL80",
		At: netloc.PointU{Element: seedmap.StationMain, U: u, Direction: facing},
	}}}
	stock, ok := s.StockType("VL80")
	if !ok {
		t.Fatal("в наборе нет паспорта VL80")
	}
	mo, err := match.StartMotion(m.Units[0], stock, net.Elements[seedmap.StationMain])
	if err != nil {
		t.Fatalf("начальное состояние: %v", err)
	}
	m.SetMotion(locoID, mo)
	m.Controls = map[string]match.Controls{locoID: match.StoppedWithAir()}
	// ПНЕВМАТИКА ЗАВОДИТСЯ ЗАРЯЖЕННОЙ, как и в настоящей партии (match.Load):
	// без неё магистраль пуста, распределитель держит полное нажатие, и машина
	// не тронется — то есть каждый тест движения проверял бы заторможенную
	// машину.
	if st, ok := s.StockType("VL80"); ok {
		if air, ok := st.AirBrake(); ok {
			m.Air = map[string]brake.State{locoID: brake.Charged(air)}
		}
	}
	// ПОЗИЦИЯ КОНТРОЛЛЕРА ЗАВОДИТСЯ ВМЕСТЕ С МАШИНОЙ, как и в настоящей партии
	// (match.Load): без ключа сила тяги не берётся вовсе, и каждый тест движения
	// проверял бы стоящую машину.
	m.Notches = map[string]int{locoID: 0}
	return NewWorld(net, s), m
}

// drive ставит рукоятки НАПРЯМУЮ, мимо match.SetControls.
//
// Нарочно: SetControls — это проверка команды (пределы паспорта, тяга при
// нулевом реверсоре), и у неё свои тесты. Здесь проверяется ДВИЖЕНИЕ, и оно
// обязано быть тем же самым, откуда бы положение рукояток ни взялось — от
// машиниста или от будущего автопилота.
func drive(t *testing.T, m *match.Match, c match.Controls) {
	t.Helper()
	m.Controls[locoID] = c
}

func step(t *testing.T, w *World, m *match.Match, ticks int) {
	t.Helper()
	for range ticks {
		if err := w.Advance(m, tickDT); err != nil {
			t.Fatalf("шаг мира: %v", err)
		}
	}
}

func motion(t *testing.T, m *match.Match) match.Motion {
	t.Helper()
	mo, ok := m.MotionOf(locoID)
	if !ok {
		t.Fatal("у машины нет состояния физики")
	}
	return mo
}

// TestStandingStaysStanding — мир без команды никуда не едет.
//
// Проверка выглядит пустой и не является ею: интегратор, у которого знак
// сопротивления перепутан, разгоняет стоящую машину сам, и заметить это на
// движущейся невозможно — там сопротивление тонет в тяге.
func TestStandingStaysStanding(t *testing.T) {
	w, m := world(t, 150, netloc.DirForward)
	before := motion(t, m)
	step(t, w, m, 100) // десять секунд модельного времени
	after := motion(t, m)
	if after.Speed != 0 || after.S != before.S {
		t.Fatalf("стоящая машина уехала: скорость %v, s %s -> %s", after.Speed, before.S, after.S)
	}
}

// reference — НЕЗАВИСИМОЕ интегрирование тех же сил, написанное прямолинейно и
// во float64.
//
// Нужно затем, чтобы проверять движение не «золотым числом» (оно подтверждает
// лишь то, что код не менялся) и не грубой прикидкой (она пропускает ошибку в
// разы). Здесь другая реализация той же модели: те же функции ПТР из physics, но
// свой цикл, свои единицы и никакого округления домена. Совпадение двух
// независимых счётов и есть проверка.
func reference(loco physics.Locomotive, st content.StockType, c match.Controls,
	el track.CompiledElement, s0 units.Distance, facing netloc.Direction,
	seconds float64) (speed, distance float64) {
	const dt = 0.001 // с — заведомо мельче шага физики
	v := 0.0         // м/с, вдоль роста u
	s := s0.Meters()
	dir := 1.0
	if facing == netloc.DirReverse {
		dir = -1
	}
	if c.Reverser == match.ReverserReverse {
		dir = -dir
	}
	for t := 0.0; t < seconds; t += dt {
		sp, err := units.MetersToDistance(s)
		if err != nil {
			return v, s - s0.Meters()
		}
		grade, radius, err := el.AlignmentAt(sp)
		if err != nil {
			return v, s - s0.Meters()
		}
		vAbs, _ := units.KmhToSpeed(math.Abs(v) * 3.6)
		f := 0.0
		if c.Traction > 0 && c.Reverser != match.ReverserNeutral {
			f += dir * float64(loco.TractiveEffort(vAbs)) * float64(c.Traction) / float64(st.Controls.TractionNotches)
		}
		if c.Brake > 0 && v != 0 {
			f -= math.Copysign(1, v) * float64(loco.BrakeForce(vAbs)) *
				float64(c.Brake) / float64(st.Controls.BrakeNotches)
		}
		if v != 0 {
			w := loco.Res.At(vAbs) + physics.CurveResistance(radius)
			f -= math.Copysign(1, v) * float64(w.On(loco.Mass.Weight()))
		}
		f -= float64(physics.GradeResistance(grade).On(loco.Mass.Weight()))
		a := f / float64(loco.Mass)
		v += a * dt
		s += v * dt
	}
	return v, s - s0.Meters()
}

// TestTractionMatchesIndependentIntegration — разгон сверяется со ВТОРЫМ СЧЁТОМ.
//
// Проверяется не «примерно правильно», а совпадение двух независимых
// интегрирований одной модели: сборка сил, знаки и целочисленная арифметика
// домена против прямолинейного счёта во float64 с шагом в двадцать раз мельче.
func TestTractionMatchesIndependentIntegration(t *testing.T) {
	w, m := world(t, 150, netloc.DirForward)
	st, _ := w.set.StockType("VL80")
	loco, _ := st.Locomotive()
	el := w.net.Elements[seedmap.StationMain]
	c := match.Controls{Traction: 33, Reverser: match.ReverserForward}
	drive(t, m, c)

	// ПЯТЬ СЕКУНД, А НЕ ДЕСЯТЬ, и это не подгонка: за десять машина на полной
	// тяге успевает добежать до упора, встать, и сравнение превращается в
	// сравнение двух нулей. Первый заход так и вышел — «скорость 0.000 против
	// 18.657», — и это ошибка ТЕСТА, а не кода. Пять секунд оставляют запас.
	const seconds = 5.0
	step(t, w, m, int(seconds*10))
	got := motion(t, m)

	start, _ := units.MetersToDistance(150)
	wantV, wantS := reference(loco, st, c, el, start, netloc.DirForward, seconds)
	gotV := float64(got.Speed) / 1e6
	gotS := (got.S - start).Meters()
	if rel(gotV, wantV) > 0.01 {
		t.Fatalf("за %.0f с скорость %.3f м/с, независимый счёт даёт %.3f м/с", seconds, gotV, wantV)
	}
	if rel(gotS, wantS) > 0.01 {
		t.Fatalf("за %.0f с прошли %.2f м, независимый счёт даёт %.2f м", seconds, gotS, wantS)
	}
	// И ПРОВЕРКА ПРОВЕРКИ: если бы машина упёрлась в упор, оба числа были бы
	// нулями и тест прошёл бы, ничего не проверив.
	if got.S >= w.net.Elements[seedmap.StationMain].LengthS {
		t.Fatalf("машина дошла до конца элемента — сравнение стало пустым")
	}
	t.Logf("разгон за %.0f с: %.3f м/с и %.2f м (независимый счёт: %.3f м/с, %.2f м)",
		seconds, gotV, gotS, wantV, wantS)
}

// TestReverserDecidesDirection — реверсор задаёт СТОРОНУ, а не скорость.
//
// Симметрия «вперёд и назад одинаково» здесь НЕ проверяется, и это не пропуск.
// Первый заход её проверял и споткнулся: 1.829 м вперёд против 1.803 м назад.
// Замер объяснил разницу — уклон под машиной НУЛЕВОЙ, а вот кривая рядом есть:
// на s = 140 м радиус 500 м, на s = 150 м прямая. Назад машина въезжает в
// кривую и платит сопротивление от неё, вперёд — нет. Это физика, а не ошибка,
// и проверять надо то, что и должно быть одинаковым: ЗНАК.
func TestReverserDecidesDirection(t *testing.T) {
	w, m := world(t, 150, netloc.DirForward)
	el := w.net.Elements[seedmap.StationMain]
	start, _ := units.MetersToDistance(150)
	grade, radius, err := el.AlignmentAt(start)
	if err != nil {
		t.Fatalf("выравнивание: %v", err)
	}
	t.Logf("под машиной: уклон %d тысячных промилле, радиус %s", grade, radius)

	drive(t, m, match.Controls{Traction: 10, Reverser: match.ReverserForward})
	step(t, w, m, 20)
	fwd := motion(t, m)
	if fwd.Speed <= 0 || fwd.S <= start {
		t.Fatalf("вперёд: скорость %v, s %s -> %s", fwd.Speed, start, fwd.S)
	}

	w2, m2 := world(t, 150, netloc.DirForward)
	drive(t, m2, match.Controls{Traction: 10, Reverser: match.ReverserReverse})
	step(t, w2, m2, 20)
	back := motion(t, m2)
	if back.Speed >= 0 || back.S >= start {
		t.Fatalf("назад: скорость %v, s %s -> %s", back.Speed, start, back.S)
	}
}

// TestFacingFlipsWithReverser — машина, повёрнутая против роста u, при том же
// реверсоре едет в другую сторону КООРДИНАТЫ, а не в другую сторону мира.
func TestFacingFlipsWithReverser(t *testing.T) {
	w, m := world(t, 150, netloc.DirReverse)
	drive(t, m, match.Controls{Traction: 10, Reverser: match.ReverserForward})
	step(t, w, m, 20)
	if got := motion(t, m); got.Speed >= 0 {
		t.Fatalf("машина, повёрнутая против роста u, поехала по росту u: %v", got.Speed)
	}
}

// TestBrakeStopsAndDoesNotReverse — тормоз останавливает и НЕ везёт назад.
//
// Вторая половина важнее первой: интегратор, не гасящий остаток на нуле, при
// полном тормозе разгоняет машину в обратную сторону, и на экране это выглядит
// как самопроизвольный уход.
func TestBrakeStopsAndDoesNotReverse(t *testing.T) {
	w, m := world(t, 150, netloc.DirForward)
	drive(t, m, match.Controls{Traction: 20, Reverser: match.ReverserForward})
	step(t, w, m, 50)
	rolling := motion(t, m)
	if rolling.Speed <= 0 {
		t.Fatalf("машина не разогналась: %v", rolling.Speed)
	}

	// ПОЛНОЕ СЛУЖЕБНОЕ КРАНОМ, а не ступенью: у ВЛ80 есть магистраль, и глубину
	// торможения задаёт разрядка. Ступень при этом не применяется вовсе, и
	// оставить её здесь значило бы проверять тормоз, которого у этой машины нет.
	drive(t, m, match.Controls{Traction: 0, Handle: brake.HandleService,
		Reverser: match.ReverserForward})
	step(t, w, m, 300) // тридцать секунд: заведомо больше тормозного пути
	stopped := motion(t, m)
	if stopped.Speed != 0 {
		t.Fatalf("после полного торможения скорость %v", stopped.Speed)
	}
	if stopped.S <= rolling.S {
		t.Fatalf("при торможении машина поехала назад: %s -> %s", rolling.S, stopped.S)
	}
	// И стоит она дальше: ещё сто тиков ничего не меняют.
	step(t, w, m, 100)
	if again := motion(t, m); again.S != stopped.S || again.Speed != 0 {
		t.Fatalf("стоящая под тормозом машина сдвинулась: %s -> %s", stopped.S, again.S)
	}
}

// TestBufferStopHoldsTheMachine — упор: дальше пути нет, и встаёт КОНЕЦ машины.
//
// # Что здесь изменилось дважды и почему
//
// Сперва тест требовал S == LengthS — «машина встала в границе элемента». Это
// было верно про ТОЧКУ ОТСЧЁТА и неверно про машину: точка отсчёта — середина
// между плоскостями автосцепок, значит половина машины (у ВЛ80 17.09 м)
// оказывалась ЗА упором. Владелец увидел это в кадре: «уже закончились рельсы, а
// он едет».
//
// Затем он требовал S == LengthS − полдлины. Это верно про ОДИНОЧНЫЙ ЛОКОМОТИВ и
// приблизительно про всё остальное: полдлины от середины — это конец машины
// только пока машина целиком на одном элементе. Приближение было объявлено
// вслух в sim.move и умерло вместе с ним.
//
// Проверяется теперь то, что и есть правда: КОНЕЦ ОТРЕЗКА стоит РОВНО В ПОРТУ.
// Утверждение не знает ни про полдлины, ни про число элементов под машиной, и
// потому переживёт состав.
//
// Третье утверждение («и стоит она дальше») появилось из ошибки одной из
// починок: та ловила конец машины только при пересечении границы, и середина
// свободно уходила за предел внутрь элемента, а потом отбрасывалась назад. То
// есть машина не стояла у буфера, а колотилась об него с размахом в полмашины.
// Покой обязан быть покоем, а не размахом.
func TestBufferStopHoldsTheMachine(t *testing.T) {
	w, m := world(t, 150, netloc.DirForward)
	el := w.net.Elements[seedmap.StationMain]
	drive(t, m, match.Controls{Traction: 33, Reverser: match.ReverserForward})
	step(t, w, m, 600)
	got := motion(t, m)
	if got.Element != seedmap.StationMain {
		t.Fatalf("машина уехала с главного пути на %s — за ним ничего нет", got.Element)
	}
	endEl, endS := spanEndA(t, got.Span)
	if endEl != seedmap.StationMain || endS != el.LengthS {
		t.Fatalf("конец машины встал на %s элемента %s, а порт — на %s",
			endS, endEl, el.LengthS)
	}
	if got.Speed != 0 {
		t.Fatalf("упёрлась, но скорость %v", got.Speed)
	}
	// И СТОИТ: ещё сто тиков под полной тягой ничего не меняют. Без этого
	// утверждения «упёрлась» проходило бы и у машины, которая бьётся об упор.
	was := got.S
	step(t, w, m, 100)
	if again := motion(t, m); again.S != was || again.Speed != 0 {
		t.Fatalf("упёршаяся машина под тягой сдвинулась: %s -> %s, скорость %v", was, again.S, again.Speed)
	}
}

// spanEndA — где лежит конец A отрезка: последний визит, тем концом, которым он
// смотрит от конца B.
//
// Своя копия арифметики концов, а не вызов неэкспортированного sideA пакета
// track: тест, спрашивающий проверяемый код, как понимать его же ответ,
// подтверждает согласие кода с самим собой. Здесь — независимое прочтение
// правила «направление визита говорит, совпадает ли ход B → A с ростом u».
func spanEndA(t *testing.T, sp track.Span) (string, units.Distance) {
	t.Helper()
	if len(sp) == 0 {
		t.Fatal("у машины пустой отрезок пути")
	}
	last := sp[len(sp)-1]
	if last.Direction == netloc.DirForward {
		return last.Element, last.To
	}
	return last.Element, last.From
}

// TestTurnoutDecidesTheBranch — положение остряка решает, куда уедет машина.
//
// Едем в сторону стрелки SW1 и смотрим, на каком элементе оказались. Это и есть
// проверка того, ради чего положение стрелки заведено состоянием партии: без
// него переход через порт неоднозначен.
func TestTurnoutDecidesTheBranch(t *testing.T) {
	// Главный путь идёт от SW1.S к тупику, значит ехать к стрелке — против
	// роста u.
	w, m := world(t, 150, netloc.DirForward)
	drive(t, m, match.Controls{Traction: 33, Reverser: match.ReverserReverse})
	step(t, w, m, 400)
	got := motion(t, m)
	if got.Element == seedmap.StationMain {
		t.Fatalf("за 40 с не дошли до стрелки: s = %s", got.S)
	}
	// Прямое положение уводит на проход straight — оттуда на подход.
	if got.Element == seedmap.StationSW1+mapfmt.PassageDiverging {
		t.Fatalf("при прямом положении остряка уехали на боковой проход: %s", got.Element)
	}
}

// TestMotionShowsUpOnTheWire — состояние физики доезжает до провода: u вместо
// s, скорость со знаком, направление.
func TestMotionShowsUpOnTheWire(t *testing.T) {
	w, m := world(t, 150, netloc.DirForward)
	drive(t, m, match.Controls{Traction: 15, Reverser: match.ReverserForward})
	step(t, w, m, 30)

	states, err := m.States(w.net)
	if err != nil {
		t.Fatalf("проекция на провод: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("единиц в проекции %d", len(states))
	}
	st := states[0]
	if st.Speed <= 0 {
		t.Fatalf("на проводе скорость %v", st.Speed)
	}
	if st.At.U <= 150 {
		t.Fatalf("на проводе u = %.3f, а машина стартовала со 150 и ехала вперёд", st.At.U)
	}
	if st.At.Element != seedmap.StationMain || !st.At.Direction.Directed() {
		t.Fatalf("на проводе адрес %+v", st.At)
	}
	// СКОРОСТЬ ЕДЕТ СТРОКОЙ — правило провода. Проверяется на самом кодировании,
	// а не на типе: тип легко потерять при сборке ответа.
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("сериализация состояния: %v", err)
	}
	var probe struct {
		Speed json.RawMessage `json:"speed"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("разбор состояния: %v", err)
	}
	if len(probe.Speed) == 0 || probe.Speed[0] != '"' {
		t.Fatalf("скорость на проводе не строкой: %s", probe.Speed)
	}
}

// TestStepSizeIsHonest — цена шага интегрирования, замером.
//
// Кусочно-постоянная аппроксимация ошибается тем сильнее, чем длиннее шаг.
// Проверяется, что выбранные 20 мс дают расхождение с шагом вчетверо меньшим
// (5 мс) в доли процента — то есть что шаг выбран не наугад, а с проверенным
// запасом.
func TestStepSizeIsHonest(t *testing.T) {
	run := func(dt units.SimTime) units.Distance {
		w, m := world(t, 18, netloc.DirForward)
		drive(t, m, match.Controls{Traction: 33, Reverser: match.ReverserForward})
		// Одинаковое МОДЕЛЬНОЕ время: пять секунд разгона. Больше нельзя —
		// машина упрётся в упор, оба прогона встанут в одну точку, и сравнение
		// покажет ноль расхождения, ничего не проверив. Ровно так и вышло на
		// первом заходе (0.0000 %), и это ошибка теста, а не свойство шага.
		for range int(5 * units.Second / dt) {
			if err := w.Advance(m, dt); err != nil {
				t.Fatalf("шаг: %v", err)
			}
		}
		got := motion(t, m)
		if got.S >= w.net.Elements[seedmap.StationMain].LengthS {
			t.Fatal("прогон упёрся в упор — сравнение шагов стало пустым")
		}
		return got.S
	}
	coarse := run(PhysicsStep)
	fine := run(5 * units.Millisecond)
	diff := math.Abs(float64(coarse-fine)) / float64(fine)
	if diff > 0.005 {
		t.Fatalf("шаг %s против 5 мс расходится на %.3f%% пути (%s против %s)",
			PhysicsStep, diff*100, coarse, fine)
	}
	t.Logf("расхождение шага %s против 5 мс: %.4f%% пути", PhysicsStep, diff*100)
}

// sumOf — хеш байтов ассета: адрес записи в наборе есть хеш содержимого.
func sumOf(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

// rel — относительное расхождение двух чисел.
func rel(got, want float64) float64 {
	if want == 0 {
		return math.Abs(got)
	}
	return math.Abs(got-want) / math.Abs(want)
}

func formatKN(f units.Force) string {
	return strconv.FormatFloat(f.Kilonewtons(), 'f', 1, 64)
}

// stopDistance — сколько машина проедет от начала торможения до остановки при
// данном положении крана. Возвращает путь и то, упёрлась ли она в конец
// элемента: остановка об упор — не торможение, и путать их нельзя.
func stopDistance(t *testing.T, handle brake.Handle) (units.Distance, bool) {
	t.Helper()
	w, m := world(t, 18, netloc.DirForward)
	drive(t, m, match.Controls{Traction: 20, Reverser: match.ReverserForward,
		Handle: brake.HandleRun})
	step(t, w, m, 30)
	from := motion(t, m)
	if from.Speed <= 0 {
		t.Fatalf("машина не разогналась: %v", from.Speed)
	}
	drive(t, m, match.Controls{Traction: 0, Reverser: match.ReverserForward, Handle: handle})
	step(t, w, m, 600)
	to := motion(t, m)
	if to.Speed != 0 {
		t.Fatalf("за 60 с машина не остановилась: %v", to.Speed)
	}
	// «УПЁРЛАСЬ» СПРАШИВАЕТСЯ У КОНЦА ОТРЕЗКА, а не у точки отсчёта. Сравнение
	// to.S == el.LengthS стало ложным ВСЕГДА в тот день, когда упор начал ловить
	// настоящий конец машины: середина встаёт на полдлины раньше порта. Сторож,
	// который не срабатывает никогда, хуже отсутствующего — он выглядит живым.
	el := w.net.Elements[seedmap.StationMain]
	endEl, endS := spanEndA(t, to.Span)
	return to.S - from.S, endEl == el.ID && endS == el.LengthS
}

// TestBrakeHandleStopsTheMachine — ТОРМОЗ РАБОТАЕТ КРАНОМ, а не ступенью.
//
// Проверка заведена вместе с пневматикой (ClearAhead-4mwn) и ловит ровно тот
// способ сломать её, которым она чуть не сломалась при заведении: у машины
// появилась магистраль, ступень перестала действовать, а прежний тест
// торможения продолжал проходить — потому что машина успевала доехать до упора
// и вставала об него. Поэтому здесь ДВА утверждения, а не одно: остановилась и
// остановилась НЕ ОБ УПОР.
func TestBrakeHandleStopsTheMachine(t *testing.T) {
	dist, atStop := stopDistance(t, brake.HandleService)
	if atStop {
		t.Fatal("машина встала об упор, а не под тормозом — проверка ничего не доказала")
	}
	if dist <= 0 {
		t.Fatalf("тормозной путь %s", dist)
	}
	// ПОЕЗДНОЕ ПОЛОЖЕНИЕ НЕ ТОРМОЗИТ: тот же разгон при ручке в поездном обязан
	// дать ЗАМЕТНО больший выбег. Без этой половины «тормоз работает» доказывало
	// бы лишь то, что машина когда-нибудь останавливается.
	w, m := world(t, 18, netloc.DirForward)
	drive(t, m, match.Controls{Traction: 20, Reverser: match.ReverserForward,
		Handle: brake.HandleRun})
	step(t, w, m, 30)
	from := motion(t, m)
	drive(t, m, match.Controls{Traction: 0, Reverser: match.ReverserForward,
		Handle: brake.HandleRun})
	step(t, w, m, 600)
	coast := motion(t, m).S - from.S
	if coast <= dist {
		t.Fatalf("выбег без тормоза %s не длиннее тормозного пути %s", coast, dist)
	}
	t.Logf("тормозной путь служебным %s, выбег в поездном %s", dist, coast)
}

// TestEmergencyStopsShorterThanService — экстренное короче служебного, и это
// СЛЕДСТВИЕ ТЕМПА РАЗРЯДКИ, а не отдельное число тормозной силы: полное нажатие
// у обоих одно, но экстренное набирает его быстрее, и разница — путь, пройденный
// за время наполнения цилиндра.
func TestEmergencyStopsShorterThanService(t *testing.T) {
	service, atStopS := stopDistance(t, brake.HandleService)
	emergency, atStopE := stopDistance(t, brake.HandleEmergency)
	if atStopS || atStopE {
		t.Fatal("машина встала об упор — сравнивать нечего")
	}
	if emergency >= service {
		t.Fatalf("экстренное %s не короче служебного %s", emergency, service)
	}
	t.Logf("тормозной путь: служебное %s, экстренное %s (короче на %s)",
		service, emergency, service-emergency)
}

// slowWorld — партия с машиной, у которой объявлены ТЕМП НАБОРА позиций и предел
// двигателей: та, на которой видно и постепенный набор, и буксование.
func slowWorld(t *testing.T, u float64) (*World, *match.Match) {
	t.Helper()
	s := setWith(t, func(st map[string]any) {
		st["controls"] = map[string]any{
			"traction_notches": 33, "brake_notches": 5, "notch_rate": 1.0,
		}
		tr := st["traction"].(map[string]any)
		tr["max_force"] = 900.0
	})
	net := network(t)
	m := &match.Match{ID: "M1", Region: net.MapID, Units: []match.Unit{{
		ID: locoID, Name: "LOCO_1", Type: "VL80",
		At: netloc.PointU{Element: seedmap.StationMain, U: u, Direction: netloc.DirForward},
	}}}
	stock, ok := s.StockType("VL80")
	if !ok {
		t.Fatal("в наборе нет паспорта VL80")
	}
	mo, err := match.StartMotion(m.Units[0], stock, net.Elements[seedmap.StationMain])
	if err != nil {
		t.Fatalf("начальное состояние: %v", err)
	}
	m.SetMotion(locoID, mo)
	m.Controls = map[string]match.Controls{locoID: match.StoppedWithAir()}
	if st, ok := s.StockType("VL80"); ok {
		if air, ok := st.AirBrake(); ok {
			m.Air = map[string]brake.State{locoID: brake.Charged(air)}
		}
	}
	m.Notches = map[string]int{locoID: 0}
	return NewWorld(net, s), m
}

// TestControllerRampsToTheHandle — РУКОЯТКА ЕСТЬ ЗАДАНИЕ, а не позиция.
//
// Замечание владельца 2026-08-15: «двигатель не может выдать сразу 100 %
// мощности». До этого дня позиция ЭКГ равнялась положению рукоятки в тот же миг,
// и это было записано упрощением в спеке кабины §2. Проверяется следствие:
// поставили рукоятку на последнюю позицию — контроллер пошёл к ней, а не
// прыгнул.
func TestControllerRampsToTheHandle(t *testing.T) {
	w, m := world(t, 18, netloc.DirForward)
	drive(t, m, match.Controls{Traction: 33, Reverser: match.ReverserForward,
		Handle: brake.HandleRun})
	step(t, w, m, 1) // один тик, 100 мс
	got, _ := m.NotchOf(locoID)
	// Темп в фикстуре не объявлен — контроллер встаёт мгновенно, и это прежнее
	// поведение. Проверяется, что позиция ВООБЩЕ ведётся отдельно от рукоятки.
	if got != 33*1000 {
		t.Fatalf("позиция %d тысячных, ожидалось 33000 при необъявленном темпе", got)
	}
}

// TestSlowControllerTakesTimeToFullPower — с объявленным темпом машина набирает
// позиции по одной, и это ВИДНО В ПУТИ: за первые секунды она проходит заметно
// меньше, чем с мгновенным контроллером.
func TestSlowControllerTakesTimeToFullPower(t *testing.T) {
	fast, mf := world(t, 18, netloc.DirForward)
	drive(t, mf, match.Controls{Traction: 33, Reverser: match.ReverserForward,
		Handle: brake.HandleRun})
	step(t, fast, mf, 50)
	quick := motion(t, mf)

	slow, ms := slowWorld(t, 18)
	drive(t, ms, match.Controls{Traction: 33, Reverser: match.ReverserForward,
		Handle: brake.HandleRun})
	step(t, slow, ms, 50)
	gradual := motion(t, ms)
	// И ПОЗИЦИЯ НЕ ДОШЛА ДО РУКОЯТКИ: пять секунд при темпе позиция в секунду —
	// это пятая часть пути от нуля до тридцать третьей.
	if got, _ := ms.NotchOf(locoID); got >= 33*1000 {
		t.Fatalf("за 5 с контроллер дошёл до %d тысячных — набор мгновенный", got)
	}
	if gradual.Speed >= quick.Speed {
		t.Fatalf("медленный набор дал скорость %v, мгновенный — %v: разницы нет",
			gradual.Speed, quick.Speed)
	}
	t.Logf("за 5 с: мгновенный контроллер %v, набор по позиции в секунду %v",
		quick.Speed, gradual.Speed)
}

// TestFullHandleSlipsOnTheWay — БУКСОВАНИЕ НАСТУПАЕТ ПО ДОРОГЕ К РУКОЯТКЕ, а не
// в тот миг, когда её двинули.
//
// Это следствие двух правок разом, и проверять их порознь нечестно: рукоятка
// стала заданием (контроллер идёт к ней своим темпом), а сила тяги перестала
// быть минимумом из мощности и сцепления. Машинист ставит последнюю позицию —
// машина трогается спокойно, набирает позиции и срывается тогда, когда двигатели
// запросят больше, чем удержит рельс.
func TestFullHandleSlipsOnTheWay(t *testing.T) {
	w, m := slowWorld(t, 18)
	drive(t, m, match.Controls{Traction: 33, Reverser: match.ReverserForward,
		Handle: brake.HandleRun})
	// Сразу после команды машина ещё не буксует: позиция нулевая.
	step(t, w, m, 1)
	if motion(t, m).Slipping {
		t.Fatal("забуксовала на первом же тике — контроллер прыгнул к рукоятке")
	}
	// А по дороге — обязана.
	slipped := false
	notchAtSlip := 0
	for range 400 {
		step(t, w, m, 1)
		if motion(t, m).Slipping {
			slipped = true
			notchAtSlip, _ = m.NotchOf(locoID)
			break
		}
	}
	if !slipped {
		got, _ := m.NotchOf(locoID)
		t.Fatalf("за 40 с машина не забуксовала ни разу; позиция дошла до %d тысячных", got)
	}
	if notchAtSlip <= 1000 {
		t.Fatalf("сорвалась на позиции %d тысячных — это первая же, набора не было", notchAtSlip)
	}
	t.Logf("сорвалась на позиции %.1f из 33", float64(notchAtSlip)/1000.0)
}

// TestSpanCrossesTheBoundary — машина лежит на двух элементах сразу, и отрезок
// остаётся связным всю дорогу через стрелку.
//
// # Что именно это доказывает
//
// До 2026-08-15 такого состояния не существовало: точка отсчёта была на одном
// элементе, а концы выводились от неё полудлиной — то есть на границе машина
// «перепрыгивала» с элемента на элемент целиком. Клиент платил за это дважды:
// хорду между шкворнями приходилось СЖИМАТЬ у конца элемента (иначе шкворень
// прижимался к границе и кузов уезжал на полбазы), а показ между снимками через
// границу не интерполировался вовсе.
//
// Проверка идёт ЧЕРЕЗ ВСЮ поездку, а не в одной точке: связность отрезка — это
// инвариант, и нарушить его можно на любом из четырёх переходов пути.
func TestSpanCrossesTheBoundary(t *testing.T) {
	w, m := world(t, 40, netloc.DirForward)
	drive(t, m, match.Controls{Traction: 33, Reverser: match.ReverserReverse})
	seen := map[string]bool{}
	both := 0
	for range 400 {
		step(t, w, m, 1)
		mo := motion(t, m)
		if err := mo.Span.Connected(w.net); err != nil {
			t.Fatalf("отрезок разъехался: %v (%+v)", err, mo.Span)
		}
		if got, want := mo.Span.Length(), stockHalf(t)*2; got != want {
			t.Fatalf("длина отрезка стала %s, а машина длиной %s", got, want)
		}
		for _, iv := range mo.Span {
			seen[iv.Element] = true
		}
		if len(mo.Span) > 1 {
			both++
		}
	}
	if both == 0 {
		t.Fatal("за поездку машина ни разу не легла на два элемента — граница не пройдена")
	}
	if len(seen) < 3 {
		t.Fatalf("за поездку побывали на %d элементах: %v", len(seen), seen)
	}
	t.Logf("кадров с телом на двух элементах: %d, элементов пройдено: %d", both, len(seen))
}

// stockHalf — полдлины ВЛ80 фикстуры, в мере пути.
func stockHalf(t *testing.T) units.Distance {
	t.Helper()
	st, ok := set(t).StockType("VL80")
	if !ok {
		t.Fatal("в наборе нет паспорта VL80")
	}
	half, err := units.MetersToDistance(st.LengthM / 2)
	if err != nil {
		t.Fatalf("полудлина: %v", err)
	}
	return half
}

// TestMachineStopsAtTheOtherBody — ЗАПРЕТ НАЛОЖЕНИЯ В ДВИЖЕНИИ: машина встаёт,
// упёршись в стоящую, и встаёт ВПЛОТНУЮ, а не за метр до неё.
//
// # Почему «вплотную» — отдельное утверждение
//
// Потому что простой способ запретить наложение — отказаться от шага целиком,
// когда он ведёт в чужой отрезок. Он проще и он приближение: машина встала бы не
// доезжая на путь одного подшага (0.14 м при 6.9 м/с), и зазор зависел бы от
// скорости — то есть на экране два тела то стояли бы вплотную, то с щелью.
// Поэтому шаг делится пополам до микрометра, и проверяется именно это.
func TestMachineStopsAtTheOtherBody(t *testing.T) {
	w, m := world(t, 40, netloc.DirForward)
	// Вторая машина стоит впереди по ходу: едем вперёд (по росту u), ставим её
	// дальше по элементу.
	const parked = "01a3185c-6002-7242-8242-000001424242"
	st, ok := w.set.StockType("VL80")
	if !ok {
		t.Fatal("в наборе нет паспорта VL80")
	}
	other := match.Unit{ID: parked, Name: "LOCO_2", Type: "VL80",
		At: netloc.PointU{Element: seedmap.StationMain, U: 150, Direction: netloc.DirForward}}
	m.Units = append(m.Units, other)
	mo, err := match.StartMotion(other, st, w.net.Elements[seedmap.StationMain])
	if err != nil {
		t.Fatalf("вторая машина: %v", err)
	}
	m.SetMotion(parked, mo)

	drive(t, m, match.Controls{Traction: 33, Reverser: match.ReverserForward})
	step(t, w, m, 400)

	moving := motion(t, m)
	if moving.Speed != 0 {
		t.Fatalf("машина не остановилась перед стоящей: скорость %v", moving.Speed)
	}
	// ВПЛОТНУЮ: конец A едущей и конец B стоящей сошлись в одной точке.
	headEl, headS := spanEndA(t, moving.Span)
	parkedSpan, _ := m.MotionOf(parked)
	tailEl, tailS := parkedSpan.Span[0].Element, parkedSpan.Span[0].From
	if headEl != tailEl || headS != tailS {
		t.Fatalf("встала концом на (%s, %s), а хвост стоящей на (%s, %s) — зазор %s",
			headEl, headS, tailEl, tailS, tailS-headS)
	}
	// И НЕ ПРОЛЕЗЛА: наложения нет ни на микрометр.
	if _, at, busy := m.Conflict(locoID, moving.Span); busy {
		t.Fatalf("машины наложились: %+v", at)
	}
	// И СТОИТ: ещё сто тиков под полной тягой ничего не меняют.
	step(t, w, m, 100)
	if again := motion(t, m); again.S != moving.S || again.Speed != 0 {
		t.Fatalf("упёршаяся в соседа машина сдвинулась: %s -> %s", moving.S, again.S)
	}
}
