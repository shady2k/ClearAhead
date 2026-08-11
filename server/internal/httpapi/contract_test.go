package httpapi_test

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http/httptest"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/httpapi"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/mapstore"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// wireNetwork — контракт с клиентом, записанный отдельным типом: тело
// GET /regions/{region}/revisions/{n}/network.
//
// Это не копия track.RenderGeometry ради копии: доменный тип может меняться, а
// провод — нет без осознанного решения. Декодирование идёт с
// DisallowUnknownFields, поэтому лишнее поле в ответе сервера роняет тест, а не
// доезжает до клиента незамеченным.
//
// Переезд ресурса под корень региона (ClearAhead-8kx) тела не тронул: сменились
// адрес и имя ресурса, байты остались те же. Следом (ClearAhead-z4u) сменились и
// корневые поля тела: были map_id и map_revision, стали region и revision — тело
// обязано описывать тот ресурс, который спросили, а манифест региона рядом уже
// отдавал region и revision, и клиент видел две системы имён в соседних ответах.
// Регион и карта — одно и то же по решению world-storage §3 («map_id обозначает
// РЕГИОН, а не станцию»), поэтому переименование ничего не переадресовало.
//
// Тогда же у элемента появился обязательный `kind` со значением "rail": ресурс
// network называет класс содержимого, автомобильные дороги приедут в этот же
// ответ, и различитель заводится ЗАРАНЕЕ — поле в персистентных данных дешевле
// всего добавить вслепую и дороже всего мигрировать (разбор в mapfmt.KindRail).
type wireNetwork struct {
	Region             string          `json:"region"`
	Revision           int             `json:"revision"`
	Elements           []wireElement   `json:"elements"`
	Trackside          []wireTrackside `json:"trackside"`
	TrackTypes         []wireTrackType `json:"track_types"`
	ConstructionRuns   []wireRun       `json:"construction_runs"`
	Features           []wireFeature   `json:"features"`
	PlacementAlgorithm string          `json:"placement_algorithm"`
}

type wireTrackType struct {
	ID      string      `json:"id"`
	Gauge   float64     `json:"gauge"`
	Sleeper wireSleeper `json:"sleeper"`
	Ballast wireBallast `json:"ballast"`
}

type wireSleeper struct {
	Pitch  float64 `json:"pitch"`
	Length float64 `json:"length"`
	Width  float64 `json:"width"`
}

type wireBallast struct {
	HalfWidth float64 `json:"half_width"`
}

type wireRun struct {
	ID         string        `json:"id"`
	Type       string        `json:"type"`
	Coordinate string        `json:"coordinate"`
	Phase      float64       `json:"phase"`
	Spans      []wireRunSpan `json:"spans"`
}

type wireRunSpan struct {
	Element   string  `json:"element"`
	From      float64 `json:"from"`
	To        float64 `json:"to"`
	Direction string  `json:"direction"`
}

type wireFeature struct {
	Owner     string        `json:"owner"`
	Kind      string        `json:"kind"`
	Point     wirePoint     `json:"point"`
	Addresses []wireAddress `json:"addresses"`
}

type wirePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type wireAddress struct {
	Element string    `json:"element"`
	U       float64   `json:"u"`
	Tangent wirePoint `json:"tangent"`
}

type wireElement struct {
	ID    string          `json:"id"`
	Kind  string          `json:"kind"`
	Start wireStart       `json:"start"`
	Prims []wirePrimitive `json:"primitives"`
	Role  *wireRole       `json:"role"`
}

type wireRole struct {
	Turnout string `json:"turnout"`
	Branch  string `json:"branch"`
	Hand    string `json:"hand"`
	Frog    string `json:"frog,omitempty"`
}

type wireTrackside struct {
	ID     string     `json:"id"`
	Kind   string     `json:"kind"`
	Side   string     `json:"side"`
	Offset float64    `json:"offset,omitempty"`
	Width  float64    `json:"width,omitempty"`
	Spans  []wireSpan `json:"spans"`
}

type wireSpan struct {
	Element string  `json:"element"`
	From    float64 `json:"from"`
	To      float64 `json:"to"`
}

type wireStart struct {
	Plan  wirePose `json:"plan"`
	Z     float64  `json:"z"`
	Slope float64  `json:"slope"`
}

type wirePose struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Heading float64 `json:"heading"`
}

type wirePrimitive struct {
	Kind   string  `json:"kind"`
	Length float64 `json:"length"`
	Radius float64 `json:"radius,omitempty"`
	Angle  float64 `json:"angle,omitempty"`
}

// ЗДЕСЬ БЫЛ ЭТАЛОН — contract/render_geometry.golden.json и сверка провода с
// ним байт в байт. Снят 2026-08-11 вместе с клиентом, по двум независимым
// причинам.
//
// Первая: эталон существовал для ДВУХ сторон — «клиент на Godot читает тот же
// файл и проверяет, что умеет его разобрать». Клиента нет, и провод, форму
// которого эталон замораживал, переписывается целиком (чанки, отсчёты рельефа,
// явные позиции сущностей). Замораживать форму, о которой уже решено, что она
// изменится, — это не контракт, а бухгалтерия.
//
// Вторая, и она важнее: СВЕРКА БАЙТ В БАЙТ ВЫЧИСЛЕННЫХ float64 НЕ ПЕРЕЖИВАЕТ
// СМЕНЫ МАШИНЫ. Проверено: тест падает на коммите 2622e05, которым сам эталон и
// записан, — на неизменном коде. Расхождение ~1e-13 в последних разрядах восьми
// чисел (координаты крестовин), то есть FMA и порядок вычислений на другой
// архитектуре, а не чья-то правка. Бида ClearAhead-3hj винила ветку
// b1-server-half — ветка ни при чём.
//
// Когда контракт понадобится снова, сверять придётся С ДОПУСКОМ либо округляя
// провод до объявленной точности, но не байтами.

// TestWireContractDecodesStrictly — ответ сервера обязан укладываться в
// объявленный wire-тип без остатка.
func TestWireContractDecodesStrictly(t *testing.T) {
	raw := renderStation(t)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var w wireNetwork
	if err := dec.Decode(&w); err != nil {
		t.Fatalf("ответ сервера не укладывается в объявленный контракт: %v", err)
	}
	if w.Region == "" || len(w.Elements) == 0 {
		t.Fatalf("контракт декодировался, но пуст: %+v", w)
	}
	for _, e := range w.Elements {
		if e.ID == "" || len(e.Prims) == 0 {
			t.Fatalf("элемент без ID или без примитивов: %+v", e)
		}
		// Вид обязателен У КАЖДОГО элемента, включая проходы стрелок: клиент
		// разбирает его с первого дня, а не выводит из адреса ресурса.
		if e.Kind != mapfmt.KindRail {
			t.Fatalf("элемент %s: вид %q, ожидался %q", e.ID, e.Kind, mapfmt.KindRail)
		}
		for _, p := range e.Prims {
			switch p.Kind {
			case "straight":
			case "arc":
				if p.Radius <= 0 || p.Angle == 0 {
					t.Fatalf("дуга без радиуса или угла: %+v", p)
				}
			default:
				t.Fatalf("неизвестный примитив в проводе: %q", p.Kind)
			}
			if p.Length <= 0 {
				t.Fatalf("примитив нулевой длины в проводе: %+v", p)
			}
		}
	}
	// Роли: у ветвей стрелки роль с полными данными, у обычных путей её нет.
	// Клиент не должен разбирать ID — значит, контракт обязан нести всё.
	var roles int
	for _, e := range w.Elements {
		if e.Role == nil {
			continue
		}
		roles++
		r := e.Role
		if r.Turnout == "" || r.Branch == "" || r.Hand == "" {
			t.Fatalf("роль без стрелки/ветви/руки: %+v", r)
		}
		switch r.Branch {
		case "straight", "diverging":
		default:
			t.Fatalf("неизвестная ветвь в роли: %q", r.Branch)
		}
		switch r.Hand {
		case "right", "left":
		default:
			t.Fatalf("неизвестная рукость в роли: %q", r.Hand)
		}
	}
	if roles == 0 {
		t.Fatal("ни один элемент не получил роль ветви стрелки")
	}

	// Путевые объекты: спаны в координате u, как в карте. Unknown kind не
	// пройдёт — клиент рисует только то, что знает.
	if len(w.Trackside) == 0 {
		t.Fatal("в контракте нет trackside")
	}
	for _, ts := range w.Trackside {
		if ts.ID == "" {
			t.Fatal("путевой объект без ID")
		}
		switch ts.Kind {
		case "platform", "buffer_stop":
		default:
			t.Fatalf("неизвестный kind путевого объекта: %q", ts.Kind)
		}
		if ts.Kind == "platform" && (ts.Offset <= 0 || ts.Width <= 0) {
			t.Fatalf("платформа %s без размеров: offset %g, width %g", ts.ID, ts.Offset, ts.Width)
		}
		if len(ts.Spans) == 0 {
			t.Fatalf("путевой объект %s без спанов", ts.ID)
		}
		for _, s := range ts.Spans {
			if s.Element == "" || s.From < 0 || s.To < s.From {
				t.Fatalf("путевой объект %s: неверный спан %+v", ts.ID, s)
			}
		}
	}

	// Рецепт решётки (спека §3–4): типы и run'ы с ЯВНЫМ типом, версия
	// алгоритма размещения. Клиент скрытого умолчания не применяет никогда.
	if w.PlacementAlgorithm == "" {
		t.Fatal("в контракте нет placement_algorithm")
	}
	if len(w.TrackTypes) == 0 {
		t.Fatal("в контракте нет типов путевой конструкции")
	}
	for _, tt := range w.TrackTypes {
		if tt.ID == "" || tt.Gauge <= 0 || tt.Sleeper.Pitch <= 0 ||
			tt.Sleeper.Length <= 0 || tt.Sleeper.Width <= 0 || tt.Ballast.HalfWidth <= 0 {
			t.Fatalf("тип без формы: %+v", tt)
		}
	}
	if len(w.ConstructionRuns) == 0 {
		t.Fatal("в контракте нет run'ов размещения")
	}
	for _, r := range w.ConstructionRuns {
		if r.ID == "" || r.Type == "" || r.Coordinate != "u" {
			t.Fatalf("run без явного типа или не в координате u: %+v", r)
		}
		if r.Phase < 0 || len(r.Spans) == 0 {
			t.Fatalf("run с неверной фазой или без спанов: %+v", r)
		}
		for _, s := range r.Spans {
			if s.Element == "" || s.From < 0 || s.To <= s.From {
				t.Fatalf("run %s: неверный спан %+v", r.ID, s)
			}
			switch s.Direction {
			case "forward", "reverse":
			default:
				t.Fatalf("run %s: направление %q", r.ID, s.Direction)
			}
		}
	}

	// Особенности уровня 2 (спека §5): крестовины с адресами и касательными.
	if len(w.Features) == 0 {
		t.Fatal("в контракте нет особенностей")
	}
	for _, f := range w.Features {
		if f.Owner == "" || f.Kind != "frog" || len(f.Addresses) != 2 {
			t.Fatalf("особенность без формы: %+v", f)
		}
		for _, a := range f.Addresses {
			if a.Element == "" || a.U < 0 {
				t.Fatalf("адрес особенности без формы: %+v", a)
			}
			if norm := a.Tangent.X*a.Tangent.X + a.Tangent.Y*a.Tangent.Y; math.Abs(norm-1) > 1e-6 {
				t.Fatalf("касательная адреса не единичная: %+v (норма %g)", a.Tangent, norm)
			}
		}
	}
}

// compileFixture грузит карту-фикстуру и компилирует её — общий вход для тестов
// провода и манифеста.
func compileFixture(t *testing.T) (*mapfmt.Map, *track.CompiledTrack, *track.RenderGeometry) {
	t.Helper()
	m := seedmap.Station()
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("валидация: %v", err)
	}
	ct, rg, err := track.Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	return m, ct, rg
}

// renderStation компилирует карту-фикстуру и сериализует сеть так же, как это
// делает ручка /regions/{region}/revisions/{n}/network.
func renderStation(t *testing.T) []byte {
	t.Helper()
	_, _, rg := compileFixture(t)
	// Байты берутся там же, где их берёт ручка и где считается ETag. Отступы
	// добавляются только для читаемости диффа: эталон обязан быть выводим из
	// отдаваемого тела, иначе он описывает не то, что уходит клиенту.
	body, err := track.RenderBody(rg)
	if err != nil {
		t.Fatalf("сериализация: %v", err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		t.Fatalf("форматирование: %v", err)
	}
	return pretty.Bytes()
}

// TestManifestFromFixture — ручка отдаёт манифест скомпилированной фикстуры:
// клиент не знает map_id и ревизию заранее и берёт их из этого ответа, поэтому
// манифест обязан доехать без искажений и без лишних полей.
func TestManifestFromFixture(t *testing.T) {
	m, ct, rg := compileFixture(t)
	man, err := track.BuildManifest(m, ct, rg)
	if err != nil {
		t.Fatalf("манифест: %v", err)
	}
	// Фикстура попадает в каталог карт и в память сервера через реальный путь
	// загрузки — манифест ручки обязан совпасть с посчитанным напрямую.
	s := mapstore.Open()
	if _, err := s.Set(m); err != nil {
		t.Fatalf("карта не прошла вход: %v", err)
	}
	h := httpapi.NewHandler(s)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/manifest", nil))
	if w.Code != 200 {
		t.Fatalf("код %d, ожидалось 200", w.Code)
	}
	dec := json.NewDecoder(w.Body)
	dec.DisallowUnknownFields()
	var got track.Manifest
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("манифест не укладывается в track.Manifest: %v", err)
	}
	if got != man {
		t.Fatalf("манифест %+v, ожидался %+v", got, man)
	}
	// Сверяем с самой картой, а не с литералом: манифест обязан описывать ту
	// карту, из которой получен, и переименование фикстуры не должно требовать
	// правки теста.
	if got.MapID != m.MapID || got.Revision != m.MapRevision {
		t.Fatalf("манифест карты %q ревизии %d, ожидалась %q ревизии %d",
			got.MapID, got.Revision, m.MapID, m.MapRevision)
	}
}
