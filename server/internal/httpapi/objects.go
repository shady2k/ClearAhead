package httpapi

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/mapstore"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// RenderBuilding — постройка в проводе.
//
// Z везётся ЯВНО и считается сервером: направление авторитета здесь обратное
// пути — путь диктует отметку земле, а дом её принимает. Считать её на клиенте
// нельзя: замер спеки объектов даёт расхождение отметки между уровнем 0 и
// уровнем 4 в среднем 0.39 м и до 2.30 м, то есть дом висел бы в воздухе или
// тонул в зависимости от того, какой чанк клиент успел загрузить.
type RenderBuilding struct {
	ID      string  `json:"id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Z       float64 `json:"z"`
	Heading float64 `json:"heading"`
	Width   float64 `json:"width"`
	Depth   float64 `json:"depth"`
	Height  float64 `json:"height"`
}

// RenderRiver — река в проводе: ось с отметкой уреза и замеренной шириной.
//
// # Что здесь есть и чего намеренно нет
//
// ЕСТЬ: где течёт, на какой отметке и докуда доходит вода. Это мир — второй
// клиент обязан показать реку там же и такой же.
//
// НЕТ ГЛУБИНЫ И ШИРИНЫ БЕРЕГА. Оба числа уже сделали свою работу на сервере:
// русло врезано в высоты, и форма берега приезжает клиенту в отсчётах чанка,
// не стоя ни одного лишнего байта. Слать их значило бы предложить клиенту
// повторить врезку — то есть ровно ту вторую реализацию, против которой заведено
// правило «рецепта клиент не видит никогда».
//
// НЕТ ЦВЕТА, БЛЕСКА И РЯБИ. Вода выглядит так, как решит рендерер, — по той же
// границе, по которой он выбирает цвет луга и меш ели.
//
// УРЕЗ ВЕЗЁТСЯ ЗАМЕРЕННЫМ, А НЕ ОДНИМ ЧИСЛОМ НА РЕКУ. Первая редакция несла
// «полуширину глади» константой, и это была неправда: вода стоит там, где земля
// поднялась до её отметки, а земля — шум плюс врезка. На пологом берегу вода
// заходит дальше, на крутом ближе, и река постоянной ширины выдаёт себя
// каналом. Сервер идёт по лучу от оси и находит урез (terrain.WaterEdge),
// отдельно влево и вправо: берега у реки разные.
type RenderRiver struct {
	ID   string             `json:"id"`
	Axis []RenderRiverPoint `json:"axis"`
}

// RenderRiverPoint — точка оси: план, отметка уреза и куда вода доходит.
//
// HalfLeft и HalfRight — расстояния от оси до уреза по левую и правую руку,
// если смотреть по ходу оси. Клиент строит по ним ленту и ничего не считает.
type RenderRiverPoint struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Z         float64 `json:"z"`
	HalfLeft  float64 `json:"half_left"`
	HalfRight float64 `json:"half_right"`
}

// RenderObjects — тело ресурса семантических объектов региона.
//
// Массивы непустые даже у карты без блока objects: форма контракта — «[]», а не
// null. То же правило, что у track_types и construction_runs.
type RenderObjects struct {
	Region    string           `json:"region"`
	Revision  int              `json:"revision"`
	Buildings []RenderBuilding `json:"buildings"`
	Rivers    []RenderRiver    `json:"rivers"`
}

// BuildObjects переносит объекты карты в провод, сажая их на рабочую
// поверхность.
//
// Поле рельефа может отсутствовать — карта без рельефа законна. Тогда отметка
// нулевая, и это честно: сажать не на что.
func BuildObjects(m *mapfmt.Map, f *terrain.Field) *RenderObjects {
	out := &RenderObjects{
		Region:    m.MapID,
		Revision:  m.MapRevision,
		Buildings: []RenderBuilding{},
		Rivers:    []RenderRiver{},
	}
	if m.Objects == nil {
		return out
	}
	for _, r := range m.Objects.Rivers {
		out.Rivers = append(out.Rivers, RenderRiver{ID: r.ID, Axis: riverAxis(r, f)})
	}
	for _, b := range m.Objects.Buildings {
		z := 0.0
		if f != nil {
			z = f.WorkedM(b.X, b.Y)
		}
		out.Buildings = append(out.Buildings, RenderBuilding{
			ID: b.ID, X: b.X, Y: b.Y, Z: z, Heading: b.Heading,
			Width: b.Width, Depth: b.Depth, Height: b.Height,
		})
	}
	return out
}

// WriteObjects отдаёт тело ресурса.
//
// Кэш — no-cache, как у манифеста региона и у чанка: адрес называет ревизию, но
// отметка земли меняется вместе с рельефом, а он переписывается при прокладке
// пути. immutable здесь соврал бы ровно так же, как на чанке (ClearAhead-5vr).
func WriteObjects(w http.ResponseWriter, o *RenderObjects) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, no-cache")
	_ = json.NewEncoder(w).Encode(o)
}

// objectsAPI — ручка семантических объектов региона.
//
// Отметка земли считается ЗДЕСЬ, а не при входе карты в память, и это цена,
// названная вслух: поле рельефа строится на каждый запрос. Сегодня построек
// двенадцать и это незаметно; когда их станут тысячи, отметку придётся считать
// один раз при входе карты — там же, где сериализуется сеть. Заводить кэш
// сейчас значило бы завести его до того, как он понадобился.
type objectsAPI struct {
	store *mapstore.Store
}

// NewObjectsHandler собирает ручку объектов.
func NewObjectsHandler(store *mapstore.Store) http.Handler {
	return &objectsAPI{store: store}
}

func (a *objectsAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "regions" || parts[2] != "revisions" || parts[4] != "objects" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "только GET и HEAD", http.StatusMethodNotAllowed)
		return
	}
	rev, err := strconv.Atoi(parts[3])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	st, ok := a.store.Current()
	if !ok || st.Map.MapID != parts[1] || st.Map.MapRevision != rev {
		http.NotFound(w, r)
		return
	}
	// Поле рельефа нужно только ради отметки; карта без рельефа законна, и
	// тогда отметка нулевая — это честно, сажать не на что.
	var field *terrain.Field
	if st.Map.Terrain != nil {
		_, els, perr := track.Propagate(&st.Map)
		if perr == nil {
			field, _ = terrain.New(&st.Map, els)
		}
	}
	WriteObjects(w, BuildObjects(&st.Map, field))
}

// riverAxis переносит ось русла в провод, замеряя урез в каждой точке.
//
// Нормаль берётся по СОСЕДНИМ точкам (центральной разностью, а на концах —
// односторонней): та же схема, которой считается нормаль рельефа, и по той же
// причине — направление в точке есть свойство линии, а не отрезка.
//
// Поле рельефа может отсутствовать: карта без рельефа законна. Тогда урез
// замерить не по чему, и полуширины остаются нулями — река вырождается в линию.
// Это честно: воды без земли не бывает, и подставить сюда полуширину русла
// значило бы нарисовать ленту, висящую в пустоте.
func riverAxis(r mapfmt.River, f *terrain.Field) []RenderRiverPoint {
	out := make([]RenderRiverPoint, 0, len(r.Axis))
	maxOut := r.HalfWidthM + r.BankM
	for i, p := range r.Axis {
		rp := RenderRiverPoint{X: p.X, Y: p.Y, Z: p.Z}
		if f != nil {
			tx, ty := riverTangent(r.Axis, i)
			// Левая нормаль к ходу оси: поворот касательной на +90°.
			nx, ny := -ty, tx
			rp.HalfLeft = f.WaterEdge(p.X, p.Y, nx, ny, p.Z, maxOut)
			rp.HalfRight = f.WaterEdge(p.X, p.Y, -nx, -ny, p.Z, maxOut)
		}
		out = append(out, rp)
	}
	return out
}

// riverTangent — единичное направление оси в точке i.
func riverTangent(axis []mapfmt.RiverPoint, i int) (tx, ty float64) {
	lo, hi := i-1, i+1
	if lo < 0 {
		lo = i
	}
	if hi >= len(axis) {
		hi = i
	}
	dx, dy := axis[hi].X-axis[lo].X, axis[hi].Y-axis[lo].Y
	n := math.Hypot(dx, dy)
	if n == 0 {
		return 1, 0
	}
	return dx / n, dy / n
}
