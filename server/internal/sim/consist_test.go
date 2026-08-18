package sim

// consist_test.go — СЦЕП ИЗ НЕСКОЛЬКИХ МАШИН: то, ради чего движущимся телом
// стал сцеп, а не единица (В4).
//
// Одиночная машина ничего из этого не показывает: у неё сумма сил равна её
// собственной, а «идти как одно тело» не с кем. Проверяется здесь ровно то, что
// появилось вместе с составом — жёсткость, общая масса и общая остановка.

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/brake"
	"github.com/shady2k/ClearAhead/server/internal/content"
	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

const (
	gonAID = "01a3185c-6003-7242-8242-000002424242"
	gonBID = "01a3185c-6004-7242-8242-000003424242"
)

// Габарит машин фикстуры — те же числа, что в боевом наборе: длины складываются
// в расстановке, и разойдись они с боевыми, тест мерил бы другой состав.
const (
	locoLenM = 32.84
	gonLenM  = 13.92
)

// setWithWagon — набор из локомотива и полувагона.
//
// Полувагон нужен ИМЕННО ГРУЖЁНЫЙ: сцеп проверяется массой, а масса вагона
// зависит от груза (content.StockType.AtLoad). Порожний вагон весит вчетверо
// меньше гружёного, и состав из трёх машин разгонялся бы почти как локомотив —
// то есть проверка сложения масс ничего бы не поймала.
func setWithWagon(t *testing.T) *content.Set {
	t.Helper()
	dir := t.TempDir()
	body := []byte("не glb, подрезка не запрашивается")
	if err := os.WriteFile(filepath.Join(dir, "x.bin"), body, 0o600); err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	loco := setWith(t, nil).Stock[0] // паспорт ВЛ80 фикстуры целиком, без второго описания
	var locoDoc map[string]any
	raw, _ := json.Marshal(loco)
	if err := json.Unmarshal(raw, &locoDoc); err != nil {
		t.Fatalf("фикстура: паспорт локомотива: %v", err)
	}
	// organs выводится сервером и автором не пишется: паспорт, вернувшийся из
	// набора, его уже несёт, и обратно он не принимается.
	delete(locoDoc, "organs")

	doc := map[string]any{
		"format_version": content.FormatVersion,
		"assets": []any{map[string]any{
			"name": "vid", "file": "x.bin", "media_type": "application/octet-stream",
			"source_hash": content.Addr(hex.EncodeToString(sumOf(body))),
			"anchor":      "rail_top_gauge_center", "scale": 1.0, "translation": []any{0, 0, 0},
			"attribution": map[string]any{"title": "T", "author": "A", "source": "S",
				"license": "CC0-1.0", "modified": false},
		}},
		"stock": []any{locoDoc, map[string]any{
			"id": "GON", "length": gonLenM, "bogie_base": 8.65, "width": 3.158, "height": 3.80,
			"max_speed":  120.0,
			"freight":    map[string]any{"tare": 24.0, "payload": 70.0, "axles": 4},
			"resistance": map[string]any{"a": 1.0, "b": 0.042, "c": 0.00016},
			"resistance_loaded": map[string]any{"base": 0.7, "k0": 3.0, "k1": 0.1, "k2": 0.0025,
				"empty_below_q0": 6.0},
			"brake": map[string]any{"shoes": "cast_iron", "braked_axles": 4,
				"axle_force_empty": 34.32, "axle_force_loaded": 68.65},
			"appearance": "vid",
		}},
	}
	out, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(dir, content.FileName), out, 0o600); err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	s, err := content.Load(dir)
	if err != nil {
		t.Fatalf("набор: %v", err)
	}
	return s
}

// train — партия «локомотив и два гружёных полувагона за ним», сцепленные в
// один сцеп, и мир движения к ней.
//
// Машины стоят ВПЛОТНУЮ: конец одной в точности там же, где конец соседней.
// Это и есть сцеп геометрически, и заодно проверка того, что сосед по составу
// не считается препятствием (match.ConflictExcept): при полуоткрытых интервалах
// касание наложением не является, но проехать вперёд, упираясь в собственный
// хвост, машина не смогла бы.
func train(t *testing.T, load float64) (*World, *match.Match) {
	t.Helper()
	net := network(t)
	s := setWithWagon(t)

	// Локомотив головой по росту u, вагоны — за ним, то есть на меньших u.
	at := func(u float64) netloc.PointU {
		return netloc.PointU{Element: seedmap.StationMain, U: u, Direction: netloc.DirForward}
	}
	// СТАРТ ДАЛЬШЕ ОТ УПОРА, ЧЕМ У ОДИНОЧНОЙ МАШИНЫ: за десять секунд полной
	// тяги локомотив успевает добежать до конца пути и встать (об этом
	// предупреждает и TestTractionMatchesIndependentIntegration), а состав
	// длиной шестьдесят метров упрётся туда хвостом ещё раньше. Сравнение двух
	// вставших тел проверяло бы упор, а не сцеп.
	locoU := 70.0
	gonAU := locoU - locoLenM/2 - gonLenM/2
	gonBU := gonAU - gonLenM

	m := &match.Match{ID: "M1", Region: net.MapID, Units: []match.Unit{
		{ID: locoID, Name: "LOCO_1", Type: "VL80", At: at(locoU)},
		{ID: gonAID, Name: "GON_1", Type: "GON", At: at(gonAU), LoadT: &load},
		{ID: gonBID, Name: "GON_2", Type: "GON", At: at(gonBU), LoadT: &load},
	}}
	for _, u := range m.Units {
		st, ok := s.StockType(u.Type)
		if !ok {
			t.Fatalf("в наборе нет паспорта %s", u.Type)
		}
		mo, err := match.StartMotion(u, st, net.Elements[seedmap.StationMain])
		if err != nil {
			t.Fatalf("начальное состояние %s: %v", u.Name, err)
		}
		m.SetMotion(u.ID, mo)
	}
	// СЦЕП: от конца B к концу A. Конец A — тот, куда смотрит локомотив, значит
	// порядок «хвостовой вагон, головной вагон, локомотив». Перевёрнутых нет:
	// все три поставлены одним направлением.
	m.SetConsist(match.Consist{
		ID: "TRAIN", Leading: match.EndA,
		Members: []match.Member{{UnitID: gonBID}, {UnitID: gonAID}, {UnitID: locoID}},
	})
	m.Controls = map[string]match.Controls{locoID: match.StoppedWithAir()}
	if st, ok := s.StockType("VL80"); ok {
		if air, ok := st.AirBrake(); ok {
			m.Air = map[string]brake.State{locoID: brake.Charged(air)}
		}
	}
	m.Notches = map[string]int{locoID: 0}
	return NewWorld(net, s), m
}

// gap — расстояние между точками отсчёта двух единиц вдоль u.
func gap(t *testing.T, m *match.Match, a, b string) float64 {
	t.Helper()
	ma, ok := m.MotionOf(a)
	if !ok {
		t.Fatalf("нет состояния у %s", a)
	}
	mb, ok := m.MotionOf(b)
	if !ok {
		t.Fatalf("нет состояния у %s", b)
	}
	if ma.Element != mb.Element {
		t.Fatalf("единицы на разных элементах: %s и %s", ma.Element, mb.Element)
	}
	return math.Abs(float64(ma.S-mb.S)) / 1e6
}

// СЦЕП ИДЁТ КАК ОДНО ТЕЛО: расстояние между машинами не меняется ни на
// микрометр, и все три едут.
//
// Жёсткость проверяется ЧИСЛОМ, а не тем, что «все сдвинулись»: состав, у
// которого голова уходит вперёд быстрее хвоста, тоже сдвинул бы всех — и
// растянулся бы на метр за минуту хода.
func TestConsistMovesAsOneBody(t *testing.T) {
	w, m := train(t, 70)
	before := []float64{gap(t, m, locoID, gonAID), gap(t, m, gonAID, gonBID)}
	drive(t, m, match.Controls{Traction: 33, Reverser: match.ReverserForward, Handle: brake.HandleRun})
	step(t, w, m, 50)

	for _, id := range []string{locoID, gonAID, gonBID} {
		mo, _ := m.MotionOf(id)
		if mo.Speed == 0 {
			t.Fatalf("единица %s стоит, хотя сцеп едет", id)
		}
	}
	after := []float64{gap(t, m, locoID, gonAID), gap(t, m, gonAID, gonBID)}
	for i := range before {
		if math.Abs(after[i]-before[i]) > 1e-6 {
			t.Fatalf("расстояние в сцепе изменилось: было %.6f м, стало %.6f м", before[i], after[i])
		}
	}
	c, ok := m.ConsistOf(locoID)
	if !ok {
		t.Fatal("сцеп потерян")
	}
	t.Logf("за 5 с сцеп разогнался до %.3f м/с, зазоры %.3f и %.3f м",
		float64(c.Speed)/1e6, after[0], after[1])
}

// ГРУЖЁНЫЙ СОСТАВ РАЗГОНЯЕТСЯ МЕДЛЕННЕЕ ПОРОЖНЕГО, И ЭТО СЧИТАЕТСЯ МАССОЙ, а не
// объявляется.
//
// Тот же локомотив, та же ступень, та же дистанция — разная загрузка вагонов.
// Массы: порожний состав 192 + 24 + 24 = 240 т, гружёный 192 + 94 + 94 = 380 т.
// Отношение масс 1.58, и разгон обязан отличаться в ту же сторону; точного
// отношения скоростей ждать нельзя — сопротивление у гружёного вагона считается
// по другой формуле ПТР (по нагрузке от оси), и это не пропорция.
func TestLoadedConsistAcceleratesSlower(t *testing.T) {
	speedAfter := func(load float64) units.Speed {
		w, m := train(t, load)
		drive(t, m, match.Controls{Traction: 33, Reverser: match.ReverserForward, Handle: brake.HandleRun})
		step(t, w, m, 50)
		c, ok := m.ConsistOf(locoID)
		if !ok {
			t.Fatal("сцеп потерян")
		}
		return c.Speed
	}
	empty := speedAfter(0)
	full := speedAfter(70)
	if empty <= 0 || full <= 0 {
		t.Fatalf("состав не тронулся: порожний %v, гружёный %v", empty, full)
	}
	if full >= empty {
		t.Fatalf("гружёный состав разогнался не медленнее порожнего: %.3f против %.3f м/с",
			float64(full)/1e6, float64(empty)/1e6)
	}
	t.Logf("за 5 с: порожний %.3f м/с, гружёный %.3f м/с (массы 240 и 380 т)",
		float64(empty)/1e6, float64(full)/1e6)
}

// ОДИНОЧНЫЙ ЛОКОМОТИВ РАЗГОНЯЕТСЯ БЫСТРЕЕ, ЧЕМ ОН ЖЕ С СОСТАВОМ.
//
// Проверка того, что сложение масс и сопротивлений вообще случилось: до В4
// вагоны не влияли на локомотив никак, и этот тест был бы зелёным при полностью
// сломанном сцепе — обе скорости совпали бы.
func TestConsistIsHeavierThanLocomotiveAlone(t *testing.T) {
	wAlone, mAlone := world(t, 70, netloc.DirForward)
	drive(t, mAlone, match.Controls{Traction: 33, Reverser: match.ReverserForward, Handle: brake.HandleRun})
	step(t, wAlone, mAlone, 50)
	alone, _ := mAlone.ConsistOf(locoID)

	wTrain, mTrain := train(t, 70)
	drive(t, mTrain, match.Controls{Traction: 33, Reverser: match.ReverserForward, Handle: brake.HandleRun})
	step(t, wTrain, mTrain, 50)
	withTrain, _ := mTrain.ConsistOf(locoID)

	if withTrain.Speed >= alone.Speed {
		t.Fatalf("состав разогнался не медленнее одиночной машины: %.3f против %.3f м/с",
			float64(withTrain.Speed)/1e6, float64(alone.Speed)/1e6)
	}
	t.Logf("за 5 с: одиночный локомотив %.3f м/с, он же с двумя гружёными вагонами %.3f м/с",
		float64(alone.Speed)/1e6, float64(withTrain.Speed)/1e6)
}

// locoAndWagon — локомотив и одинокий гружёный полувагон ПОРОЗНЬ, каждый своим
// сцепом. Между ними gapM метров.
func locoAndWagon(t *testing.T, gapM float64) (*World, *match.Match) {
	t.Helper()
	net := network(t)
	s := setWithWagon(t)
	load := 70.0
	at := func(u float64) netloc.PointU {
		return netloc.PointU{Element: seedmap.StationMain, U: u, Direction: netloc.DirForward}
	}
	locoU := 60.0
	gonU := locoU + locoLenM/2 + gonLenM/2 + gapM
	m := &match.Match{ID: "M1", Region: net.MapID, Units: []match.Unit{
		{ID: locoID, Name: "LOCO_1", Type: "VL80", At: at(locoU)},
		{ID: gonAID, Name: "GON_1", Type: "GON", At: at(gonU), LoadT: &load},
	}}
	for _, u := range m.Units {
		st, _ := s.StockType(u.Type)
		mo, err := match.StartMotion(u, st, net.Elements[seedmap.StationMain])
		if err != nil {
			t.Fatalf("начальное состояние %s: %v", u.Name, err)
		}
		m.SetMotion(u.ID, mo)
		m.SetConsist(match.Single(u.ID))
	}
	m.Controls = map[string]match.Controls{locoID: match.StoppedWithAir()}
	if st, ok := s.StockType("VL80"); ok {
		if air, ok := st.AirBrake(); ok {
			m.Air = map[string]brake.State{locoID: brake.Charged(air)}
		}
	}
	m.Notches = map[string]int{locoID: 0}
	return NewWorld(net, s), m
}

// ПОДЪЕХАЛ — СЦЕПИЛСЯ — ПОТЯНУЛ — ОТЦЕПИЛ. Веха В4 словами: «подъезжаете,
// цепляете, тянете, расставляете, отцепляете».
//
// Тест сквозной нарочно. Порознь каждый шаг уже проверен, но между ними лежат
// швы, на которых всё и ломается: подъезд обязан ОСТАНОВИТЬ машину у чужого
// тела, сцепка — узнать смычку по той же геометрии, тяга — повезти обоих, а
// расцепка — оставить вагон стоять там, где его отцепили.
func TestApproachCoupleHaulAndPart(t *testing.T) {
	w, m := locoAndWagon(t, 12)
	wagonBefore, _ := m.MotionOf(gonAID)

	// ПОДЪЕЗД. Локомотив идёт к вагону и обязан встать, а не проехать сквозь.
	drive(t, m, match.Controls{Traction: 8, Reverser: match.ReverserForward, Handle: brake.HandleRun})
	step(t, w, m, 120)
	drive(t, m, match.Controls{Traction: 0, Reverser: match.ReverserNeutral, Handle: brake.HandleService})
	step(t, w, m, 120)
	c, _ := m.ConsistOf(locoID)
	if c.Speed != 0 {
		t.Fatalf("локомотив не встал у вагона: идёт %.3f м/с", float64(c.Speed)/1e6)
	}
	if got := gap(t, m, locoID, gonAID) - (locoLenM+gonLenM)/2; got > 0.05 {
		t.Fatalf("между машинами %.3f м — до вагона не доехали, цеплять нечего", got)
	}
	if after, _ := m.MotionOf(gonAID); after.Span.Length() != wagonBefore.Span.Length() ||
		after.S != wagonBefore.S {
		t.Fatal("вагон сдвинулся сам, хотя его ещё не цепляли")
	}

	// СЦЕПКА.
	if _, err := m.Couple(w.net, locoID, gonAID, "TRAIN"); err != nil {
		t.Fatalf("сцепка: %v", err)
	}

	// ТЯГА: теперь едут оба, и вагон — вместе с локомотивом.
	drive(t, m, match.Controls{Traction: 20, Reverser: match.ReverserForward, Handle: brake.HandleRun})
	step(t, w, m, 40)
	locoMo, _ := m.MotionOf(locoID)
	wagonMo, _ := m.MotionOf(gonAID)
	if locoMo.Speed == 0 || wagonMo.Speed == 0 {
		t.Fatalf("сцеп не поехал: локомотив %v, вагон %v", locoMo.Speed, wagonMo.Speed)
	}
	if wagonMo.S <= wagonBefore.S {
		t.Fatalf("вагон не сдвинулся с места: было %s, стало %s", wagonBefore.S, wagonMo.S)
	}
	// ОСТАНОВКА И РАСЦЕПКА. Положение вагона запоминается ПОСЛЕ остановки, а не
	// до неё: под тормозом состав проезжает ещё несколько метров, и это его
	// законный путь, а не то, что должен был бы проверять этот тест.
	drive(t, m, match.Controls{Traction: 0, Reverser: match.ReverserNeutral, Handle: brake.HandleEmergency})
	step(t, w, m, 200)
	if c, _ := m.ConsistOf(locoID); c.Speed != 0 {
		t.Fatalf("состав не остановился: %.3f м/с", float64(c.Speed)/1e6)
	}
	stopped, _ := m.MotionOf(gonAID)
	hauled := stopped.S
	locoStopped, _ := m.MotionOf(locoID)
	if _, _, err := m.Uncouple("TRAIN", locoID, "TAIL"); err != nil {
		t.Fatalf("расцепка: %v", err)
	}

	// РАЗЪЕХАЛИСЬ: локомотив уходит назад, вагон остаётся там, где стоял.
	// ТИКОВ С ЗАПАСОМ: после экстренного торможения магистраль надо зарядить
	// заново, и до отпуска колодок машина стоит под собственным тормозом. Это
	// не задержка теста, а поведение машины.
	drive(t, m, match.Controls{Traction: 15, Reverser: match.ReverserReverse, Handle: brake.HandleRun})
	step(t, w, m, 300)
	locoAfter, _ := m.MotionOf(locoID)
	wagonAfter, _ := m.MotionOf(gonAID)
	if wagonAfter.S != hauled {
		t.Fatalf("отцепленный вагон уехал: было %s, стало %s", hauled, wagonAfter.S)
	}
	if locoAfter.S >= locoStopped.S {
		t.Fatalf("локомотив не уехал назад: %s против %s", locoAfter.S, locoStopped.S)
	}
	t.Logf("вагон подвинут на %.2f м и оставлен стоять; локомотив ушёл назад",
		float64(hauled-wagonBefore.S)/1e6)
}
