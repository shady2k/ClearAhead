package content

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/units"
)

// Боевой набор репозитория проводится через полный вход — ровно тем же приёмом
// и по той же причине, что карта (mapfmt/shipped_test.go).
//
// Урок, за который заплачено раньше и который здесь применён заранее: «карту
// репозитория не грузил ни один тест, и смена версии формата сломала бы её
// незаметно». Файл набора — данные того же рода: он ссылается на хеш
// двадцатимегабайтного ассета, на имена узлов внутри чужого glTF и на перечень
// лицензий. Любая из этих связей может разойтись молча, и разойдётся она у
// того, кто запустит сервер, а не у того, кто правил файл.
const shippedContentDir = "../../assets"

func shipped(t *testing.T) *Set {
	t.Helper()
	set, err := Load(filepath.Clean(shippedContentDir))
	if err != nil {
		t.Fatalf("боевой набор не проходит вход: %v", err)
	}
	return set
}

func TestShippedContentLoads(t *testing.T) {
	set := shipped(t)
	if len(set.Assets) == 0 || len(set.Stock) == 0 {
		t.Fatalf("набор пуст: ассетов %d, паспортов %d", len(set.Assets), len(set.Stock))
	}
}

// TestShippedLocomotiveIsPlaceable — у боевого локомотива есть всё, без чего
// клиент не смог бы его ПОСТАВИТЬ, и это проверяется числами, а не наличием.
func TestShippedLocomotiveIsPlaceable(t *testing.T) {
	set := shipped(t)
	st, ok := set.StockType("VL80")
	if !ok {
		t.Fatal("паспорта VL80 в наборе нет")
	}
	// Габарит — замер меша, умноженный на масштаб подгонки под колею. Границы
	// широкие намеренно: тест стережёт не число, а его порядок. Локомотив длиной
	// 3 метра или высотой 30 — это опечатка в разряде, и она обязана падать
	// здесь, а не выглядеть на кадре игрушкой.
	if st.LengthM < 20 || st.LengthM > 60 {
		t.Fatalf("длина %.2f м вне разумного для локомотива", st.LengthM)
	}
	if st.WidthM < 2.5 || st.WidthM > 4.5 {
		t.Fatalf("ширина %.2f м вне разумного", st.WidthM)
	}
	if st.HeightM < 3 || st.HeightM > 7 {
		t.Fatalf("высота %.2f м вне разумного", st.HeightM)
	}
	a, ok := set.asset(st.Appearance)
	if !ok {
		t.Fatalf("вид %s не объявлен", st.Appearance)
	}
	if a.Anchor != "rail_top_gauge_center" {
		t.Fatalf("якорь %q: подвижной состав ставится от поверхности катания", a.Anchor)
	}
	if _, ok := set.Blob(a.Hash); !ok {
		t.Fatalf("байты вида по адресу %s не отдаются", a.Hash)
	}
}

// TestShippedLocomotiveHasTwoCabs — у ВЛ80 две кабины, по одной на секцию, и
// стоят они по концам машины.
//
// # Что здесь доказывается, а что нет
//
// Доказывается КОНТРАКТ: постов ровно два, оба на одной высоте, и они
// симметричны относительно точки отсчёта. Симметрия — сильная проверка задаром:
// числа получены из РАЗНЫХ мест файла (вторая секция повёрнута на 180° и
// отставлена), и сойтись случайно они не могут. Разъедься они — значит замер
// делали по одной кабине, а вторую досочинили.
//
// НЕ доказывается, что глаз окажется внутри нарисованной кабины: геометрии
// здесь нет, есть три числа. Это меряет зонд клиента по доехавшему мешу — там,
// где геометрия и лежит.
func TestShippedLocomotiveHasTwoCabs(t *testing.T) {
	set := shipped(t)
	st, ok := set.StockType("VL80")
	if !ok {
		t.Fatal("паспорта VL80 в наборе нет")
	}
	a, _ := set.asset(st.Appearance)
	if len(a.Cabs) != 2 {
		t.Fatalf("постов %d, у ВЛ80 две секции и две кабины", len(a.Cabs))
	}
	front := a.Placed(a.Cabs[0])
	back := a.Placed(a.Cabs[1])

	// Пол кабины — над головкой рельса и ниже крыши. Границы широкие: тест
	// стережёт порядок числа, а не само число. Пол на 0.2 м или на 4 м — это
	// опечатка, и она обязана падать здесь, а не выглядеть машинистом по пояс
	// в полу.
	for i, p := range [][3]float64{front, back} {
		if p[1] < 1.0 || p[1] > 2.5 {
			t.Fatalf("пост %d: пол кабины на %.3f м над головкой рельса — вне разумного", i, p[1])
		}
	}
	if math.Abs(front[1]-back[1]) > 0.01 {
		t.Fatalf("полы кабин на разной высоте: %.3f и %.3f м", front[1], back[1])
	}

	// СИММЕТРИЯ. Допуск 0.15 м, и он не с потолка: точка отсчёта единицы не
	// совпадает с серединой нарисованной машины ровно на 0.065 м (постановка
	// каталога подгоняет вид к модели), поэтому идеального нуля тут быть не
	// может и требовать его значило бы требовать неправды.
	if off := math.Abs(front[2] + back[2]); off > 0.15 {
		t.Fatalf("посты несимметричны: %.3f и %.3f м, середина уехала на %.3f м",
			front[2], back[2], off/2)
	}
	// По концам, а не в середине: кабина, оказавшаяся у трансформатора, — это
	// перепутанные секции.
	for i, p := range [][3]float64{front, back} {
		fromEnd := st.LengthM/2 - math.Abs(p[2])
		if fromEnd < 0.5 || fromEnd > 4.0 {
			t.Fatalf("пост %d в %.3f м от торца машины — это не кабина", i, fromEnd)
		}
	}
	if front[2]*back[2] > 0 {
		t.Fatalf("оба поста на одном конце машины: %.3f и %.3f м", front[2], back[2])
	}
}

// TestShippedLocomotiveCarriesAttribution — раздача ассета есть его
// распространение, и обязательство CC-BY просыпается у СЕРВЕРА. Тест стережёт
// не форму, а то, что уедет вместе с байтами.
func TestShippedLocomotiveCarriesAttribution(t *testing.T) {
	set := shipped(t)
	a, ok := set.asset("loco_vl80")
	if !ok {
		t.Fatal("ассета loco_vl80 в наборе нет")
	}
	at := a.Attribution
	if at.License != "CC-BY-4.0" {
		t.Fatalf("лицензия %q, у файла из репозитория CC-BY-4.0", at.License)
	}
	if !at.Modified {
		t.Fatal("сцена подрезается и масштабируется, а modified=false")
	}
	if at.Modifications == "" {
		t.Fatal("изменения не названы")
	}
}

// TestShippedLocomotiveSceneIsPruned — на кадре не должно быть кузовов,
// стоящих ни на чём. Проверяется по ОТДАННЫМ байтам: подрезка при укладке
// могла бы сработать «почти».
func TestShippedLocomotiveSceneIsPruned(t *testing.T) {
	set := shipped(t)
	a, _ := set.asset("loco_vl80")
	body, ok := set.Blob(a.Hash)
	if !ok {
		t.Fatal("байты не отдаются")
	}
	if a.Hash == a.SourceHash {
		t.Fatal("адрес совпал с хешем исходника: подрезка не применилась")
	}
	root := parseGLBJSON(t, body)["nodes"].([]any)[0]
	_ = root // корень сцены — узел 0; настоящая проверка ниже, по достижимости
	reachable := reachableNames(t, body)
	for _, gone := range []string{"vl80tk1.002_10", "vl80tk1.003_11"} {
		if reachable[gone] {
			t.Fatalf("узел %s остался достижим из сцены", gone)
		}
	}
	// И обратное: настоящая машина на месте. Иначе «подрезка» могла бы вырезать
	// всё и тест бы её похвалил.
	for _, keep := range []string{"vl80tk1_2", "bogey_3", "vl80tk1.001_9"} {
		if !reachable[keep] {
			t.Fatalf("узел %s пропал из сцены: подрезано лишнее", keep)
		}
	}
}

// reachableNames обходит сцену от корней и собирает имена достижимых узлов —
// тем же способом, каким её строит клиент.
func reachableNames(t *testing.T, body []byte) map[string]bool {
	t.Helper()
	doc := parseGLBJSON(t, body)
	nodes := doc["nodes"].([]any)
	out := map[string]bool{}
	var walk func(i int)
	walk = func(i int) {
		if i < 0 || i >= len(nodes) {
			t.Fatalf("ссылка на узел %d вне массива", i)
		}
		obj := nodes[i].(map[string]any)
		if name, ok := obj["name"].(string); ok {
			if out[name] {
				return
			}
			out[name] = true
		}
		kids, _ := obj["children"].([]any)
		for _, k := range kids {
			walk(int(k.(float64)))
		}
	}
	for _, sc := range doc["scenes"].([]any) {
		roots, _ := sc.(map[string]any)["nodes"].([]any)
		for _, r := range roots {
			walk(int(r.(float64)))
		}
	}
	return out
}

// TestShippedLocomotivePhysicsMatchesPublishedPassport — числа физики боевого
// набора против ОПУБЛИКОВАННОГО паспорта ВЛ80С.
//
// Проверяется не форма, а величины: файл набора — это данные, и опечатка в
// разряде массы или силы прошла бы любую проверку типов. Источник — статья
// «ВЛ80» русской Википедии: служебная масса ВЛ80С 192 т, конструкционная
// скорость 110 км/ч, длительный режим 40.9 тс при 53.6 км/ч.
func TestShippedLocomotivePhysicsMatchesPublishedPassport(t *testing.T) {
	set := shipped(t)
	st, ok := set.StockType("VL80")
	if !ok {
		t.Fatal("в боевом наборе нет паспорта VL80")
	}
	loco, isLoco := st.Locomotive()
	if !isLoco {
		t.Fatal("VL80 не объявлен локомотивом: блока traction нет")
	}
	if got := loco.Mass.Tonnes(); math.Abs(got-192) > 0.5 {
		t.Errorf("масса %.1f т, паспорт называет 192 т", got)
	}
	if got := loco.AdhesiveMass.Tonnes(); math.Abs(got-192) > 0.5 {
		t.Errorf("сцепной вес %.1f т, у ВЛ80 все восемь осей движущие — ожидалось 192 т", got)
	}
	if got := loco.MaxSpeed.Kmh(); math.Abs(got-110) > 0.5 {
		t.Errorf("конструкционная скорость %.1f км/ч, паспорт называет 110 км/ч", got)
	}
	// Точка длительного режима обязана воспроизводиться огибающей: 40.9 тс при
	// 53.6 км/ч. Это и есть проверка того, что сила в файле записана в
	// килоньютонах, а не в тоннах-силы, — разница вдесятеро.
	v, err := units.KmhToSpeed(53.6)
	if err != nil {
		t.Fatalf("скорость: %v", err)
	}
	if got := loco.TractiveEffort(v).TonnesForce(); math.Abs(got-40.9) > 0.15 {
		t.Errorf("на 53.6 км/ч тяга %.2f тс, паспорт называет 40.9 тс", got)
	}
	// Осевая нагрузка как независимая сверка: 192 т на восемь осей — 24 т/ось,
	// то есть в пределах допустимого для магистрального электровоза. Число
	// нагрузки в паспорт не записано, и проверять его нечем, кроме деления, —
	// поэтому проверяется порядок, а не равенство.
	if axle := loco.Mass.Tonnes() / 8; axle < 20 || axle > 26 {
		t.Errorf("нагрузка на ось %.1f т — вне правдоподобного для электровоза", axle)
	}
}

// TestShippedWagonCarriesLoad — боевой полувагон описан ДВУМЯ концами, а не
// одним числом, и всё, что зависит от загрузки, меняется вместе.
//
// Проверяется не наличие полей, а ЧИСЛА и их согласие с источником: паспорт
// вагона 12-132 (тара 24 т, грузоподъёмность 70 т, четыре оси) и таблица
// расчётных нажатий ПТР для чугунных колодок (3.5 тс на ось порожний, 7.0
// гружёный).
func TestShippedWagonCarriesLoad(t *testing.T) {
	set := shipped(t)
	st, ok := set.StockType("PV12132")
	if !ok {
		t.Fatal("паспорта PV12132 в наборе нет")
	}
	if _, isLoco := st.Locomotive(); isLoco {
		t.Fatal("полувагон объявлен локомотивом: у него не должно быть блока traction")
	}
	payload, freighted := st.Freighted()
	if !freighted {
		t.Fatal("полувагон объявлен машиной постоянного веса — груз ему возить нечем")
	}
	if math.Abs(payload-70) > 0.01 {
		t.Fatalf("грузоподъёмность %.1f т, паспорт называет 70 т", payload)
	}

	empty, err := st.AtLoad(0)
	if err != nil {
		t.Fatalf("порожний: %v", err)
	}
	full, err := st.AtLoad(payload)
	if err != nil {
		t.Fatalf("гружёный: %v", err)
	}
	if got := empty.Mass.Tonnes(); math.Abs(got-24) > 0.05 {
		t.Errorf("масса порожнего %.1f т, тара 24 т", got)
	}
	if got := full.Mass.Tonnes(); math.Abs(got-94) > 0.05 {
		t.Errorf("масса гружёного %.1f т, брутто 24+70 = 94 т", got)
	}

	// АВТОРЕЖИМ: концы отрезка — числа ПТР, середина — их половина. Половина
	// проверяется отдельно от концов, потому что концы могли бы совпасть с
	// таблицей и при ступенчатом переключении; непрерывность видна только
	// внутри отрезка.
	if got := empty.AxleBrakeForce.TonnesForce(); math.Abs(got-3.5) > 0.02 {
		t.Errorf("нажатие порожнего %.2f тс/ось, ПТР даёт 3.5", got)
	}
	if got := full.AxleBrakeForce.TonnesForce(); math.Abs(got-7.0) > 0.02 {
		t.Errorf("нажатие гружёного %.2f тс/ось, ПТР даёт 7.0", got)
	}
	half, err := st.AtLoad(payload / 2)
	if err != nil {
		t.Fatalf("полугружёный: %v", err)
	}
	if got := half.AxleBrakeForce.TonnesForce(); math.Abs(got-5.25) > 0.02 {
		t.Errorf("нажатие при половине груза %.2f тс/ось, авторежим ведёт его к середине 5.25", got)
	}

	// СОПРОТИВЛЕНИЕ ГРУЖЁНОГО СЧИТАЕТСЯ ПО НАГРУЗКЕ ОТ ОСИ, и это единственное
	// место, где видно, что форма ПТР применена, а не подменена тройкой: при
	// q0 = 94/4 = 23.5 тс/ось формула даёт w = 0.7 + (3 + 0.1V + 0.0025V²)/23.5,
	// то есть 0.828 при нулевой скорости.
	if got := float64(full.Res.At(0)) / 1000; math.Abs(got-0.828) > 0.002 {
		t.Errorf("основное сопротивление гружёного при V=0 равно %.3f Н/кН, ПТР даёт 0.828", got)
	}
	// ПОРОЖНИЙ ЭТОГО ВАГОНА СТОИТ РОВНО НА ГРАНИЦЕ, и тест сторожит именно это,
	// а не круглое число. Тара 24 т на четыре оси даёт q0 = 6.0 тс/ось, а
	// порожняя формула ПТР объявлена для q0 < 6 — то есть на тару она уже НЕ
	// распространяется, и действует гружёная: 0.7 + 3/6 = 1.2 при V = 0.
	//
	// Своя кривая порожнего при этом даёт 1.0, и разрыв в 20 % на границе —
	// свойство ПРАВИЛ, где это две разные эмпирические кривые. Проверяется
	// поведение НА ГРАНИЦЕ, потому что оно неочевидно и первый же читатель
	// решит, что порожний вагон обязан считаться порожней формулой.
	if got := float64(empty.Res.At(0)) / 1000; math.Abs(got-1.2) > 0.002 {
		t.Errorf("основное сопротивление порожнего при V=0 равно %.3f Н/кН; "+
			"при q0 = 6.0 действует гружёная формула ПТР и она даёт 1.2", got)
	}

	// ГРУЗ СВЕРХ ГРУЗОПОДЪЁМНОСТИ — ОТКАЗ, а не молча перегруженный вагон.
	if _, err := st.AtLoad(payload + 0.1); err == nil {
		t.Error("груз сверх грузоподъёмности принят")
	}
	if _, err := st.AtLoad(-1); err == nil {
		t.Error("отрицательный груз принят")
	}
}
