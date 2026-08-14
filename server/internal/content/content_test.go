package content

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSet кладёт набор в свежий каталог и возвращает путь.
func writeSet(t *testing.T, doc map[string]any, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), raw, 0o600); err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatalf("фикстура %s: %v", name, err)
		}
	}
	return dir
}

// goodDoc — исправный набор: один ассет, один паспорт на него.
func goodDoc(t *testing.T, body []byte) map[string]any {
	t.Helper()
	return map[string]any{
		"format_version": FormatVersion,
		"assets": []any{map[string]any{
			"name": "loco_x", "file": "x.glb", "media_type": "model/gltf-binary",
			"source_hash": Addr(hashOf(body)),
			"anchor":      "rail_top_gauge_center", "scale": 1.0,
			"translation": []any{0, 0, 0},
			"attribution": map[string]any{
				"title": "X", "author": "кто-то", "source": "откуда-то",
				"license": "CC0-1.0", "modified": false,
			},
		}},
		"stock": []any{map[string]any{
			"id": "X1", "length": 10.0, "bogie_base": 6.0, "width": 3.0, "height": 4.0,
			"mass": 100.0, "max_speed": 100.0,
			"resistance": map[string]any{"a": 1.9, "b": 0.01, "c": 0.0003},
			"brake":      map[string]any{"shoes": "cast_iron", "braked_axles": 4, "axle_force": 100.0},
			"traction": map[string]any{
				"adhesive_mass": 100.0, "continuous_force": 200.0, "continuous_speed": 40.0,
			},
			"controls":   map[string]any{"traction_notches": 33, "brake_notches": 5},
			"appearance": "loco_x",
		}},
	}
}

func TestLoadGoodSet(t *testing.T) {
	body := fakeGLB(t)
	set, err := Load(writeSet(t, goodDoc(t, body), map[string][]byte{"x.glb": body}))
	if err != nil {
		t.Fatalf("набор: %v", err)
	}
	if len(set.Stock) != 1 || len(set.Assets) != 1 {
		t.Fatalf("паспортов %d, ассетов %d", len(set.Stock), len(set.Assets))
	}
	a := set.Assets[0]
	if a.Hash != Addr(hashOf(body)) {
		t.Fatalf("адрес %s не равен хешу отданных байтов", a.Hash)
	}
	if a.Size != len(body) {
		t.Fatalf("size %d, байт %d", a.Size, len(body))
	}
	if _, ok := set.Blob(a.Hash); !ok {
		t.Fatalf("байты по адресу %s не отдаются", a.Hash)
	}
	if _, ok := set.Blob("sha256-" + strings.Repeat("0", 64)); ok {
		t.Fatal("выдуманный адрес отдал байты")
	}
	if set.Hash == "" {
		t.Fatal("перечень без хеша: клиенту нечем проверять кэш")
	}
}

// TestSetHashFollowsNumbers — хеш перечня обязан меняться при правке ЧИСЛА
// паспорта: иначе клиент, у которого набор в кэше, не узнает, что машина стала
// другой длины.
func TestSetHashFollowsNumbers(t *testing.T) {
	body := fakeGLB(t)
	base, err := Load(writeSet(t, goodDoc(t, body), map[string][]byte{"x.glb": body}))
	if err != nil {
		t.Fatalf("набор: %v", err)
	}
	doc := goodDoc(t, body)
	doc["stock"].([]any)[0].(map[string]any)["length"] = 11.0
	other, err := Load(writeSet(t, doc, map[string][]byte{"x.glb": body}))
	if err != nil {
		t.Fatalf("набор: %v", err)
	}
	if base.Hash == other.Hash {
		t.Fatal("длина изменилась, хеш перечня — нет")
	}
}

// TestSetHashFollowsCabs — переехавший пост обязан менять хеш перечня.
//
// Не косметика: у набора Cache-Control no-cache и ETag по этому хешу. Не
// сменись он — клиент с набором в кэше поставил бы машиниста по вчерашнему
// числу, то есть в стену, и никакой отказ этого бы не показал.
func TestSetHashFollowsCabs(t *testing.T) {
	body := fakeGLB(t)
	doc := goodDoc(t, body)
	doc["assets"].([]any)[0].(map[string]any)["cabs"] = []any{[]any{0, 1.5, 4.0}}
	base, err := Load(writeSet(t, doc, map[string][]byte{"x.glb": body}))
	if err != nil {
		t.Fatalf("набор: %v", err)
	}
	doc = goodDoc(t, body)
	doc["assets"].([]any)[0].(map[string]any)["cabs"] = []any{[]any{0, 1.5, 4.2}}
	moved, err := Load(writeSet(t, doc, map[string][]byte{"x.glb": body}))
	if err != nil {
		t.Fatalf("набор: %v", err)
	}
	if base.Hash == moved.Hash {
		t.Fatal("пост переехал, хеш перечня — нет")
	}
}

// TestLoadRefusals — валидатор отказывает, а не чинит. Каждая строка таблицы —
// испорченный набор, который обязан НЕ СОБРАТЬСЯ.
func TestLoadRefusals(t *testing.T) {
	body := fakeGLB(t)
	cases := []struct {
		name   string
		want   string
		broken func(doc map[string]any)
	}{
		// ОРГАНЫ УПРАВЛЕНИЯ (ClearAhead-6ygr). Локомотив без них — машина, которой
		// нечем управлять; вагон с ними — перепутанные строки паспорта; нулевая
		// ступень — рукоятка, которую нельзя сдвинуть.
		{"локомотив без органов", "не объявлены органы управления", func(d map[string]any) {
			delete(d["stock"].([]any)[0].(map[string]any), "controls")
		}},
		{"органы у машины без тяги", "у машины без тяги", func(d map[string]any) {
			delete(d["stock"].([]any)[0].(map[string]any), "traction")
		}},
		{"ноль ступеней тяги", "traction_notches", func(d map[string]any) {
			d["stock"].([]any)[0].(map[string]any)["controls"].(map[string]any)["traction_notches"] = 0
		}},
		{"ноль ступеней торможения", "brake_notches", func(d map[string]any) {
			d["stock"].([]any)[0].(map[string]any)["controls"].(map[string]any)["brake_notches"] = 0
		}},
		{"неизвестное поле записи", "unknown field", func(d map[string]any) {
			d["assets"].([]any)[0].(map[string]any)["colour"] = "красный"
		}},
		{"версия формата", "версия формата", func(d map[string]any) {
			d["format_version"] = FormatVersion + 1
		}},
		{"хеш исходника разошёлся", "source_hash", func(d map[string]any) {
			d["assets"].([]any)[0].(map[string]any)["source_hash"] = Addr(strings.Repeat("a", 64))
		}},
		{"атрибуция без автора", "атрибуция без поля author", func(d map[string]any) {
			delete(d["assets"].([]any)[0].(map[string]any)["attribution"].(map[string]any), "author")
		}},
		{"лицензия не SPDX", "вне перечня SPDX", func(d map[string]any) {
			d["assets"].([]any)[0].(map[string]any)["attribution"].(map[string]any)["license"] = "CC-BY"
		}},
		{"изменения не названы", "называть изменения", func(d map[string]any) {
			d["assets"].([]any)[0].(map[string]any)["attribution"].(map[string]any)["modified"] = true
		}},
		{"якоря нет", "не указан anchor", func(d map[string]any) {
			d["assets"].([]any)[0].(map[string]any)["anchor"] = ""
		}},
		{"масштаб ноль", "scale", func(d map[string]any) {
			d["assets"].([]any)[0].(map[string]any)["scale"] = 0
		}},
		{"два ассета одного имени", "объявлен дважды", func(d map[string]any) {
			a := d["assets"].([]any)[0]
			d["assets"] = []any{a, a}
		}},
		{"файл уводит из каталога", "именем файла", func(d map[string]any) {
			d["assets"].([]any)[0].(map[string]any)["file"] = "../x.glb"
		}},
		{"паспорт ссылается в пустоту", "в наборе не объявлен", func(d map[string]any) {
			d["stock"].([]any)[0].(map[string]any)["appearance"] = "нет_такого"
		}},
		{"нулевая длина машины", "length", func(d map[string]any) {
			d["stock"].([]any)[0].(map[string]any)["length"] = 0
		}},
		{"база шкворней длиннее машины", "не меньше длины", func(d map[string]any) {
			d["stock"].([]any)[0].(map[string]any)["bogie_base"] = 12.0
		}},
		{"два паспорта одного имени", "объявлен дважды", func(d map[string]any) {
			s := d["stock"].([]any)[0]
			d["stock"] = []any{s, s}
		}},
		// ФИЗИКА. Каждый случай — опечатка, которая иначе прошла бы насквозь и
		// вышла бы не отказом, а правдоподобно неверным движением.
		{"машина без массы", "mass", func(d map[string]any) {
			delete(d["stock"].([]any)[0].(map[string]any), "mass")
		}},
		{"машина без конструкционной скорости", "max_speed", func(d map[string]any) {
			delete(d["stock"].([]any)[0].(map[string]any), "max_speed")
		}},
		{"машина без сопротивления", "основное сопротивление", func(d map[string]any) {
			delete(d["stock"].([]any)[0].(map[string]any), "resistance")
		}},
		{"сцепной вес больше служебной массы", "не бывает больше массы", func(d map[string]any) {
			d["stock"].([]any)[0].(map[string]any)["traction"].(map[string]any)["adhesive_mass"] = 150.0
		}},
		{"режим быстрее конструкционной скорости", "выше конструкционной", func(d map[string]any) {
			d["stock"].([]any)[0].(map[string]any)["traction"].(map[string]any)["continuous_speed"] = 120.0
		}},
		{"нулевая сила длительного режима", "continuous_force", func(d map[string]any) {
			d["stock"].([]any)[0].(map[string]any)["traction"].(map[string]any)["continuous_force"] = 0
		}},
		{"отрицательный коэффициент сопротивления", "неотрицательное", func(d map[string]any) {
			d["stock"].([]any)[0].(map[string]any)["resistance"].(map[string]any)["b"] = -0.01
		}},
		{"подрезка без объявления изменений", "modified=false", func(d map[string]any) {
			d["assets"].([]any)[0].(map[string]any)["drop_nodes"] = []any{"spare"}
		}},
		// ПОСТ ВНЕ ГАБАРИТА. Три оси проверяются порознь нарочно: путаница осей
		// (z, записанное в поле y) проходит проверку по двум из трёх, и таблица,
		// щупающая одну ось, объявила бы её исправной.
		{"пост выше машины", "над поверхностью катания", func(d map[string]any) {
			d["assets"].([]any)[0].(map[string]any)["cabs"] = []any{[]any{0, 9.0, 0}}
		}},
		{"пост под колёсами", "над поверхностью катания", func(d map[string]any) {
			d["assets"].([]any)[0].(map[string]any)["cabs"] = []any{[]any{0, -0.5, 0}}
		}},
		{"пост за торцом машины", "вдоль хода", func(d map[string]any) {
			d["assets"].([]any)[0].(map[string]any)["cabs"] = []any{[]any{0, 2.0, 40.0}}
		}},
		{"пост сбоку от машины", "поперёк оси", func(d map[string]any) {
			d["assets"].([]any)[0].(map[string]any)["cabs"] = []any{[]any{5.0, 2.0, 0}}
		}},
		// ПОСТ ПРОВЕРЯЕТСЯ ПОСЛЕ ПОСТАНОВКИ, а не по сырому числу. Здесь сырое
		// число внутри габарита (z = 4 при длине 10), а после сдвига каталога
		// уезжает за торец — и отказ обязан случиться, иначе постановка каталога
		// проверялась бы только на клиенте, то есть глазом.
		{"пост уезжает за габарит сдвигом каталога", "вдоль хода", func(d map[string]any) {
			a := d["assets"].([]any)[0].(map[string]any)
			a["translation"] = []any{0, 0, 3.0}
			a["cabs"] = []any{[]any{0, 2.0, 4.0}}
		}},
		{"подрезка несуществующего узла", "не найден", func(d map[string]any) {
			a := d["assets"].([]any)[0].(map[string]any)
			a["drop_nodes"] = []any{"нет такого"}
			at := a["attribution"].(map[string]any)
			at["modified"] = true
			at["modifications"] = "названы"
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := goodDoc(t, body)
			c.broken(doc)
			_, err := Load(writeSet(t, doc, map[string][]byte{"x.glb": body}))
			if err == nil {
				t.Fatal("испорченный набор собрался")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("отказ %q не называет причину %q", err, c.want)
			}
		})
	}
}

// TestDropNodesChangesAddress — подрезка меняет байты, значит меняет и адрес.
// Отдавать подрезанное под адресом исходника значило бы соврать про
// содержимое — и разрушить единственный ресурс, у которого immutable честен по
// построению.
func TestDropNodesChangesAddress(t *testing.T) {
	body := fakeGLB(t)
	doc := goodDoc(t, body)
	a := doc["assets"].([]any)[0].(map[string]any)
	a["drop_nodes"] = []any{"spare"}
	at := a["attribution"].(map[string]any)
	at["modified"] = true
	at["modifications"] = "убран запасной узел"
	set, err := Load(writeSet(t, doc, map[string][]byte{"x.glb": body}))
	if err != nil {
		t.Fatalf("набор: %v", err)
	}
	got := set.Assets[0]
	if got.Hash == Addr(hashOf(body)) {
		t.Fatal("сцена подрезана, а адрес остался исходным")
	}
	if got.SourceHash != Addr(hashOf(body)) {
		t.Fatal("происхождение потеряно: source_hash обязан остаться хешем исходника")
	}
	served, _ := set.Blob(got.Hash)
	if string(served) == string(body) {
		t.Fatal("байты не изменились, хотя сцена подрезана")
	}
	// Размер НЕ проверяется, и это не пропуск: подрезка убирает ссылку из
	// дерева, а не байты геометрии. Файл при этом худеет на единицы байт и
	// может не похудеть вовсе — сборки мусора здесь нет по решению (см. glb.go).
	root := parseGLBJSON(t, served)["nodes"].([]any)[0].(map[string]any)
	if kids := root["children"].([]any); len(kids) != 1 {
		t.Fatalf("детей корня %d, ожидался один: запасной узел не отцеплен", len(kids))
	}
}

func TestLoadMissingDirRefused(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "нет-такого-каталога")); err == nil {
		t.Fatal("набора нет, а загрузка прошла")
	}
}
