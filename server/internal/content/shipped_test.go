package content

import (
	"path/filepath"
	"testing"
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
