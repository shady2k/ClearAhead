package content

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// bodyDoc — тело из одной коробки заданного размера (имя тела совпадает с именем
// ассета: файл обязан лежать под своим именем), посаженной серединой на
// половине высоты. Габарит такого тела равен его коробке, и это делает проверку
// проверкой ЧИСЛА, а не совпадения двух сложных сборок.
func bodyDoc(length, width, height float64) []byte {
	doc := map[string]any{
		"format_version": 1,
		"model":          "loco_x",
		"axes":           "x_right_y_up_z_back",
		"units":          "m",
		"angles":         "deg",
		"materials":      map[string]any{"m": map[string]any{"colour": "#808080", "roughness": 0.9, "metallic": 0.0}},
		"parts": []any{map[string]any{
			"name": "hull", "shape": "box", "material": "m",
			"at":   []any{0, height / 2, 0},
			"size": []any{width, height, length},
		}},
	}
	raw, _ := json.Marshal(doc)
	return raw
}

// setWithBody — набор одной СОГЛАСОВАННОЙ машины: паспорт и тело названы одними
// числами. Вид описан ТЕЛОМ, а не запечённым glTF: только такой вид и можно
// обмерить на сервере.
func setWithBody(t *testing.T, length, width, height float64) (*Set, error) {
	t.Helper()
	return setOf(t, length, width, height, length, width, height)
}

// setOf — набор, у которого паспорт и тело названы РАЗНЫМИ числами. Нужен ровно
// для того, чтобы проверять расхождение: у согласованной машины его не бывает.
func setOf(t *testing.T, passLen, passWid, passHgt, bodyLen, bodyWid, bodyHgt float64) (*Set, error) {
	t.Helper()
	body := bodyDoc(bodyLen, bodyWid, bodyHgt)
	doc := goodDoc(t, body)
	stock := doc["stock"].([]any)[0].(map[string]any)
	stock["length"] = passLen
	stock["width"] = passWid
	stock["height"] = passHgt
	// База шкворней внутри машины — иначе отказ придёт раньше проверки габарита,
	// и тест мерил бы не то, что задумал.
	stock["bogie_base"] = passLen * 0.6
	doc["assets"] = []any{map[string]any{
		"name": "loco_x", "file": "x.model.json", "media_type": ModelMediaType,
		"source_hash": Addr(hashOf(body)),
		"anchor":      "rail_top_gauge_center", "scale": 1.0,
		"translation": []any{0, 0, 0},
		"attribution": map[string]any{
			"title": "X", "author": "кто-то", "source": "откуда-то",
			"license": "CC0-1.0", "modified": false,
		},
	}}
	return Load(writeSet(t, doc, map[string][]byte{"x.model.json": body}))
}

// ТЕЛО, СОБРАННОЕ НЕ ПО ПАСПОРТУ, — ОТКАЗ ЗАГРУЗКИ.
//
// Паспорт фикстуры называет 10.0 × 3.0 × 4.0 м. Тело тех же чисел проходит;
// тело, у которого хоть один размер уехал, обязано быть отвергнуто — иначе
// набор объявляет одну машину, а показывает другую, и заметить это можно
// только глазом на кадре (так и вышло с первым полувагоном 2026-08-18).
func TestBodyMustMatchPassportOutline(t *testing.T) {
	if _, err := setWithBody(t, 10.0, 3.0, 4.0); err != nil {
		t.Fatalf("тело по паспорту отвергнуто: %v", err)
	}
	for _, c := range []struct {
		name            string
		l, w, h         float64
		expectInMessage string
	}{
		{"длиннее паспорта", 10.5, 3.0, 4.0, "длина"},
		{"короче паспорта", 9.6, 3.0, 4.0, "длина"},
		{"шире паспорта", 10.0, 3.3, 4.0, "ширина"},
		{"выше паспорта", 10.0, 3.0, 4.3, "высота"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := setOf(t, 10.0, 3.0, 4.0, c.l, c.w, c.h)
			if err == nil {
				t.Fatalf("тело %v × %v × %v принято при паспорте 10 × 3 × 4", c.l, c.w, c.h)
			}
			if !strings.Contains(err.Error(), c.expectInMessage) {
				t.Fatalf("отказ не назвал, ЧТО разошлось: %v", err)
			}
		})
	}
}

// ДОПУСК ЕСТЬ, И ОН МАЛ. Полсантиметра — описка в размере детали; миллиметр —
// сложение шестидесяти литералов, и отвергать его значило бы отвергать верное.
func TestBodyToleranceAcceptsMillimetreAndRefusesCentimetre(t *testing.T) {
	if _, err := setOf(t, 10.0, 3.0, 4.0, 10.0+0.001, 3.0, 4.0); err != nil {
		t.Fatalf("расхождение в миллиметр отвергнуто: %v", err)
	}
	if _, err := setOf(t, 10.0, 3.0, 4.0, 10.0+0.02, 3.0, 4.0); err == nil {
		t.Fatal("расхождение в два сантиметра принято")
	}
}

// ГАБАРИТ БОЕВОГО ПОЛУВАГОНА СЧИТАЕТСЯ ЧИСЛАМИ, а не глазами: тело обязано
// кончаться ровно на паспортной длине (её и определяют автосцепки), быть ровно
// паспортной ширины по наружной грани стоек и не уходить под головку рельса.
func TestShippedWagonBodyIsMeasured(t *testing.T) {
	set := shipped(t)
	st, ok := set.StockType("PV12132")
	if !ok {
		t.Fatal("паспорта PV12132 в наборе нет")
	}
	m, ok := set.models["gondola_12_132"]
	if !ok {
		t.Fatal("тела gondola_12_132 в наборе нет")
	}
	box, err := m.Extent(map[string]float64{
		"length": st.LengthM, "width": st.WidthM, "height": st.HeightM, "bogie_base": st.BogieBaseM,
	})
	if err != nil {
		t.Fatalf("габарит тела: %v", err)
	}
	for _, d := range []struct {
		name      string
		got, want float64
	}{
		{"длина по автосцепкам", box.Length(), 13.92},
		{"ширина по стойкам", box.Width(), 3.158},
		{"высота до верха борта", box.Height(), 3.80},
	} {
		if math.Abs(d.got-d.want) > bodyToleranceM {
			t.Errorf("%s = %.3f м, ожидалось %.3f", d.name, d.got, d.want)
		}
	}
	// Колесо касается рельса ровно в нуле: ниже поверхности катания у машины
	// нет ничего, и датум вертикали в проекте один.
	if box.MinY < -bodyToleranceM || box.MinY > bodyToleranceM {
		t.Errorf("низ тела на %.3f м от поверхности катания, ожидался ноль", box.MinY)
	}
}

// ГАБАРИТ 1-Т СТОРОЖИТ БОЕВОЙ НАБОР, а не только фикстуру.
//
// Тест написан 2026-08-18 наоборот — он ТРЕБОВАЛ, чтобы боевой набор габарит не
// проходил, потому что габарит ВЛ80 был замером чужого меша (3.63 × 5.40 м) и в
// очертание 1-Т не вписывался. Это был будильник: покраснеть в тот день, когда
// у паспорта появятся числа машины. Он и покраснел — в тот же день, через час,
// когда владелец прислал 32.84 × 3.24 × 5.10 (ClearAhead-w4q закрыта).
//
// Теперь он сторожит обратное: боевой набор габарит ПРОХОДИТ, а машина сверх
// очертания — отказ загрузки.
func TestLoadingGaugeRefusesOversizeStock(t *testing.T) {
	if err := shipped(t).CheckLoadingGauge(); err != nil {
		t.Fatalf("боевой набор не вписывается в габарит 1-Т: %v", err)
	}
	if _, err := setWithBody(t, 10.0, 3.0, 4.0); err != nil {
		t.Fatalf("машина 3.0 × 4.0 м отвергнута, хотя в габарит вписывается: %v", err)
	}
	// Шире предела — отказ ЗАГРУЗКИ, а не отдельный вызов проверки: набор с
	// такой машиной не собирается вовсе.
	_, err := setWithBody(t, 10.0, GaugeTWidthM+0.01, 4.0)
	if err == nil {
		t.Fatal("машина шире габарита 1-Т загружена")
	}
	if !strings.Contains(err.Error(), "1-Т") {
		t.Fatalf("отказ не назвал габарит: %v", err)
	}
	if _, err := setWithBody(t, 10.0, 3.0, GaugeTHeightM+0.01); err == nil {
		t.Fatal("машина выше габарита 1-Т загружена")
	}
}
