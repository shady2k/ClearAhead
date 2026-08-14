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
func set(t *testing.T) *content.Set {
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
			"brake":      map[string]any{"shoes": "cast_iron", "braked_axles": 8, "axle_force": 137.3},
			"traction": map[string]any{
				"adhesive_mass": 192.0, "continuous_force": 401.1, "continuous_speed": 53.6,
			},
			"controls":   map[string]any{"traction_notches": 33, "brake_notches": 5},
			"appearance": "vid",
		}},
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
	mo, err := match.StartMotion(m.Units[0], net.Elements[seedmap.StationMain])
	if err != nil {
		t.Fatalf("начальное состояние: %v", err)
	}
	m.SetMotion(locoID, mo)
	m.Controls = map[string]match.Controls{locoID: match.Stopped()}
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

	drive(t, m, match.Controls{Traction: 0, Brake: 5, Reverser: match.ReverserForward})
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

// TestBufferStopHoldsTheMachine — упор: дальше пути нет.
//
// Машина едет в тупиковый конец главного пути и обязана встать В ГРАНИЦЕ
// элемента, а не за ней: за границей пути не существует, и «немножко за» на
// экране выглядит как машина, висящая в воздухе.
func TestBufferStopHoldsTheMachine(t *testing.T) {
	w, m := world(t, 150, netloc.DirForward)
	el := w.net.Elements[seedmap.StationMain]
	drive(t, m, match.Controls{Traction: 33, Reverser: match.ReverserForward})
	step(t, w, m, 600)
	got := motion(t, m)
	if got.Element != seedmap.StationMain {
		t.Fatalf("машина уехала с главного пути на %s — за ним ничего нет", got.Element)
	}
	if got.S != el.LengthS {
		t.Fatalf("встала на %s, а конец элемента %s", got.S, el.LengthS)
	}
	if got.Speed != 0 {
		t.Fatalf("упёрлась, но скорость %v", got.Speed)
	}
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
		w, m := world(t, 10, netloc.DirForward)
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
