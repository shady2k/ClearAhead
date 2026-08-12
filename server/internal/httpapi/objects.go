package httpapi

import (
	"encoding/json"
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

// RenderObjects — тело ресурса семантических объектов региона.
//
// Массив непустой даже у карты без блока objects: форма контракта — «[]», а не
// null. То же правило, что у track_types и construction_runs.
type RenderObjects struct {
	Region    string           `json:"region"`
	Revision  int              `json:"revision"`
	Buildings []RenderBuilding `json:"buildings"`
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
	}
	if m.Objects == nil {
		return out
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
