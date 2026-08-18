package content

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// ЧИСЛА ТЕЛА ЗДЕСЬ НЕ СВЕРЯЮТСЯ, и это нарочно. Высота станины и раствор
// балансира — решение автора набора, и тест, повторяющий их числом, проверял бы
// только то, что файл не менялся. Проверяется ФОРМА: что описание разбирается,
// что ссылки в нём разрешимы и что испорченное описание получает отказ.

// shippedModel — модель из боевого набора. Берётся с диска, а не из фикстуры:
// смысл проверки в том, что ОТГРУЖАЕМЫЙ файл разбирается сегодняшним читателем.
func shippedModel(t *testing.T, name string) *Model {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "assets", name+".model.json"))
	if err != nil {
		t.Fatalf("файл модели: %v", err)
	}
	m, err := ParseModel(name, raw)
	if err != nil {
		t.Fatalf("разбор модели %s: %v", name, err)
	}
	return m
}

// TestShippedModelsParse — обе отгружаемые модели разбираются и называют свой
// механизм. Это и есть та связь, ради которой перечня устройств заводить не
// пришлось: файл сам говорит, чьё он тело.
func TestShippedModelsParse(t *testing.T) {
	kinds := map[string]bool{}
	for _, name := range []string{"switch_stand_manual", "switch_stand_electric"} {
		m := shippedModel(t, name)
		if !mapfmt.KnownDrive(m.Drive.Device) {
			t.Fatalf("модель %s объявила механизм %q", name, m.Drive.Device)
		}
		if kinds[m.Drive.Device] {
			t.Fatalf("механизм %s объявлен двумя моделями", m.Drive.Device)
		}
		kinds[m.Drive.Device] = true
		if m.Drive.Title == "" {
			t.Fatalf("модель %s не назвала устройство человеку", name)
		}
	}
	// Обе руки механизма покрыты: карта вправе записать любую, и стрелка без
	// тела была бы пустым местом в мире.
	for _, kind := range mapfmt.Drives {
		if !kinds[kind] {
			t.Fatalf("у механизма %s нет отгружаемого тела", kind)
		}
	}
}

// TestModelDescribesTheIndicatorByISI — ПОКАЗАНИЯ УКАЗАТЕЛЯ ЕСТЬ В ДАННЫХ, а не
// в коде клиента.
//
// Проверяется не угол (он решение автора), а то, что подвижность указателя
// объявлена по положению остряка и что у обоих положений есть свой угол. Без
// этого клиент не смог бы показать положение вовсе, а с одним углом показывал бы
// оба положения одинаково.
func TestModelDescribesTheIndicatorByISI(t *testing.T) {
	m := shippedModel(t, "switch_stand_manual")
	var byState = map[string]map[string]float64{}
	var walk func(p Part)
	walk = func(p Part) {
		if p.Pivot != nil {
			byState[p.Pivot.By] = p.Pivot.States
		}
		for _, c := range p.Parts {
			walk(c)
		}
	}
	for _, p := range m.Parts {
		walk(p)
	}
	pos, ok := byState[StatePosition]
	if !ok {
		t.Fatal("у ручного механизма нет ни одной части, подвижной по положению остряка")
	}
	for _, want := range []string{"straight", "diverging"} {
		if _, ok := pos[want]; !ok {
			t.Fatalf("положение %s не имеет угла — указатель показал бы оба положения одинаково", want)
		}
	}
	if pos["straight"] == pos["diverging"] {
		t.Fatalf("оба положения дают один угол %v — показания не различаются", pos["straight"])
	}
	if _, ok := byState[StateHand]; !ok {
		t.Fatal("стрела указателя не привязана к стороне схода — по ИСИ она направлена в сторону бокового пути")
	}
}

// TestNumberPlateFacesTheDriver — ТАБЛИЧКУ С НОМЕРОМ ЧИТАЕТ МАШИНИСТ, и ничто её
// поперёк пути не разворачивает.
//
// # Что здесь стояло и чего это стоило
//
// Щиток с номером висел на повороте по СТОРОНЕ УСТАНОВКИ (side: left 90°,
// right −90°), и довод в прежней проверке звучал так: «ни одна часть не
// привязана к стороне, с которой стоит механизм: табличка окажется в поле».
// Довод отвечал не на тот вопрос. Поворот на прямой угол разворачивает щиток
// ПОПЕРЁК пути: читать его становится тому, кто стоит в междупутье, а машинист
// видит торец в полтора сантиметра. Владелец увидел ровно это (2026-08-15):
// «табличка на стрелке повёрнута не к машинисту, а на 90 градусов».
//
// Нулевой поворот ставит щиток лицом ВДОЛЬ пути, а надпись у него набита с обеих
// сторон (label.both_sides), поэтому номер читается с обоих подходов — и стороне
// установки здесь делать нечего.
//
// Проверяется СТРУКТУРА, а не угол: угол — решение автора тела, а вот «номер не
// разворачивают поперёк» — требование к устройству, и оно проверяемо у любой
// будущей модели, как бы её ни нарисовали.
//
// Сторона установки из формата НЕ УХОДИТ: у электропривода на ней держится
// переводная тяга (часть rod), и там она означает ровно то, что должна, — куда
// механизму тянуться к остряку.
func TestNumberPlateFacesTheDriver(t *testing.T) {
	for _, name := range []string{"switch_stand_manual", "switch_stand_electric"} {
		m := shippedModel(t, name)
		var walk func(p Part, turnedBy string)
		walk = func(p Part, turnedBy string) {
			if p.Pivot != nil {
				turnedBy = p.Pivot.By
			}
			if p.Label != nil && turnedBy == StateSide {
				t.Fatalf("%s: щиток %q развёрнут по стороне установки — номер встаёт поперёк пути, и машинист видит торец",
					name, p.Name)
			}
			for _, c := range p.Parts {
				walk(c, turnedBy)
			}
		}
		for _, p := range m.Parts {
			walk(p, "")
		}
	}
}

// TestBrokenModelIsRefused — испорченное описание НЕ ЗАГРУЖАЕТСЯ.
//
// Каждый случай ловит опечатку, которая иначе доехала бы до клиента и стала
// предметом без половины деталей — то есть исправным на вид кадром.
func TestBrokenModelIsRefused(t *testing.T) {
	base := func(t *testing.T) map[string]any {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("..", "..", "assets", "switch_stand_manual.model.json"))
		if err != nil {
			t.Fatalf("файл модели: %v", err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("фикстура: %v", err)
		}
		return doc
	}
	parts := func(doc map[string]any) []any { return doc["parts"].([]any) }
	first := func(doc map[string]any) map[string]any { return parts(doc)[0].(map[string]any) }

	cases := []struct {
		name   string
		want   string
		break_ func(map[string]any)
	}{
		{"версия формата", "версия формата", func(d map[string]any) { d["format_version"] = 2.0 }},
		{"чужие оси", "соглашение об осях", func(d map[string]any) { d["axes"] = "z_up" }},
		{"неизвестный механизм", "род механизма", func(d map[string]any) {
			d["drive"].(map[string]any)["device"] = "гидравлический"
		}},
		{"без имени устройства", "не названо имя устройства", func(d map[string]any) {
			d["drive"].(map[string]any)["title"] = ""
		}},
		// ПАРАМЕТР ЭКЗЕМПЛЯРА, КОТОРОГО МОДЕЛЬ НЕ ОБЪЯВЛЯЛА. Опечатка в имени
		// привязки иначе означала бы часть без размера, собранную молча.
		{"привязка к необъявленному параметру", "модель не объявляет", func(d map[string]any) {
			first(d)["size"] = []any{
				map[string]any{"by": "ширина", "factor": 1.0, "offset": 0.0}, 0.1, 0.1}
		}},
		// ПРИВЯЗКА БЕЗ FACTOR. Пропуск неотличим от «умножить на ноль»: часть
		// схлопнулась бы в точку, и поймать это можно было бы только глазом.
		{"привязка без множителя", "без factor", func(d map[string]any) {
			d["params"] = []any{"width"}
			first(d)["size"] = []any{map[string]any{"by": "width", "offset": 0.0}, 0.1, 0.1}
		}},
		{"имя файла не то", "лежит не под тем ассетом", func(d map[string]any) { d["model"] = "другое" }},
		{"неизвестная форма", "форма", func(d map[string]any) { first(d)["shape"] = "шар" }},
		{"краска не объявлена", "в палитре модели не объявлена", func(d map[string]any) {
			first(d)["material"] = "хаки"
		}},
		{"ящик о двух размерах", "ожидалось три", func(d map[string]any) {
			first(d)["size"] = []any{1.0, 2.0}
		}},
		{"поворот вокруг двух осей", "порядок поворотов", func(d map[string]any) {
			first(d)["rotate"] = []any{10.0, 20.0, 0.0}
		}},
		{"цвет не в шестнадцатеричном", "ожидается #rrggbb", func(d map[string]any) {
			d["materials"].(map[string]any)["iron"].(map[string]any)["colour"] = "чёрный"
		}},
		{"неизвестное поле", "unknown field", func(d map[string]any) { first(d)["shine"] = 1.0 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := base(t)
			c.break_(doc)
			raw, _ := json.Marshal(doc)
			_, err := ParseModel("switch_stand_manual", raw)
			if err == nil {
				t.Fatalf("испорченное описание принято")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("отказ %q не называет причину %q", err, c.want)
			}
		})
	}
}

// TestPivotOnUnknownStateIsRefused — подвижность по состоянию, которого мир не
// присылает, — отказ, а не часть, которая никогда не двинется.
func TestPivotOnUnknownStateIsRefused(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "assets", "switch_stand_manual.model.json"))
	if err != nil {
		t.Fatalf("файл модели: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	for _, p := range doc["parts"].([]any) {
		part := p.(map[string]any)
		if pv, ok := part["pivot"].(map[string]any); ok {
			pv["by"] = "погода"
			break
		}
	}
	out, _ := json.Marshal(doc)
	_, err = ParseModel("switch_stand_manual", out)
	if err == nil || !strings.Contains(err.Error(), "которого мир не присылает") {
		t.Fatalf("подвижность по выдуманному состоянию принята: %v", err)
	}
}

// TestDriveRodStretchesToTheBlade — У ОБОИХ ПРИВОДОВ ЕСТЬ ТЯГА, И ДЛИНУ ЕЙ
// ЗАДАЁТ МИР.
//
// Проверяется не длина (её знает станция, а не тело), а СВЯЗЬ: часть объявлена
// растяжимой по величине reach. Без неё тяга остаётся тем, чем была до
// 2026-08-16, — константой в файле: у ручного привода её не было вовсе, у
// электрического она была 0.6 м при расстоянии до нитки 1.115 м, то есть висела
// в воздухе (ClearAhead-bsjq).
//
// Заодно проверяется ЕДИНИЧНОСТЬ: растяжимая часть авторится размером ровно 1
// вдоль своей оси, потому что мир этот размер умножает. Автор, записавший 0.6,
// получил бы тягу вдвое короче нужной — и ни одного отказа.
func TestDriveRodStretchesToTheBlade(t *testing.T) {
	for _, name := range []string{"switch_stand_manual", "switch_stand_electric"} {
		m := shippedModel(t, name)
		var rod *Part
		var walk func(p *Part)
		walk = func(p *Part) {
			if p.Stretch != nil {
				rod = p
			}
			for i := range p.Parts {
				walk(&p.Parts[i])
			}
		}
		for i := range m.Parts {
			walk(&m.Parts[i])
		}
		if rod == nil {
			t.Fatalf("%s: ни одной растяжимой части — тяге неоткуда взять длину", name)
		}
		if rod.Stretch.By != MeasureReach {
			t.Fatalf("%s: тяга растянута по величине %q, а не по %q", name, rod.Stretch.By, MeasureReach)
		}
		// РАСТЯЖИМАЯ ЧАСТЬ АВТОРЕНА ЕДИНИЧНОЙ: дальний край её содержимого вдоль
		// оси растяжения лежит ровно на единице, потому что мир этот размер
		// УМНОЖАЕТ. Автор, записавший 0.6, получил бы тягу вдвое короче нужной —
		// и ни одного отказа.
		//
		// Мерится ДАЛЬНИЙ КРАЙ, а не размер одной части: тяга собрана из
		// нескольких стержней (шибер и контрольные линейки), и «размер части»
		// перестал быть тем же числом, что длина группы. Первая редакция
		// проверки искала цилиндр с той же осью и на брусе-шибере отвечала нулём.
		axis := axisIndex(rod.Stretch.Axis)
		if axis < 0 {
			t.Fatalf("%s: ось растяжения %q неизвестна", name, rod.Stretch.Axis)
		}
		var far float64
		var scan func(p *Part, at float64)
		scan = func(p *Part, at float64) {
			// ТЕЛО ПРИВОДА ЖЁСТКОЕ: у него нет параметров экземпляра, и всякое
			// число здесь обязано быть литералом. Привязка означала бы, что
			// проверка меряет тело, размера которого ещё нет.
			pos := at + lit(t, name, p.At[axis])
			half := 0.0
			switch {
			case p.Shape == ShapeCylinder && p.Axis == rod.Stretch.Axis:
				half = lit(t, name, p.Height) / 2
			case p.Shape == ShapeBox && len(p.Size) > axis:
				half = lit(t, name, p.Size[axis]) / 2
			}
			if pos+half > far {
				far = pos + half
			}
			for i := range p.Parts {
				scan(&p.Parts[i], pos)
			}
		}
		for i := range rod.Parts {
			scan(&rod.Parts[i], 0)
		}
		if math.Abs(far-1) > 1e-9 {
			t.Fatalf("%s: растяжимая часть достаёт до %v вдоль %s, а обязана быть единичной",
				name, far, rod.Stretch.Axis)
		}
	}
}

// axisIndex — «x» | «y» | «z» в номер оси. Отрицательное — ось неизвестна.
func axisIndex(a string) int {
	switch a {
	case "x":
		return 0
	case "y":
		return 1
	case "z":
		return 2
	}
	return -1
}


// lit — литерал числа тела. Отказ, если число привязано к параметру экземпляра:
// у жёстких тел таких быть не может, и молчаливый ноль спрятал бы ошибку данных.
func lit(t *testing.T, model string, v Value) float64 {
	t.Helper()
	n, ok := v.Literal()
	if !ok {
		t.Fatalf("модель %s: число привязано к параметру %q, а ожидался литерал", model, v.By)
	}
	return n
}
