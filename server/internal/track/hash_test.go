package track

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

func manifestOf(t *testing.T, m *mapfmt.Map) Manifest {
	t.Helper()
	ct, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	man, err := BuildManifest(m, ct, rg)
	if err != nil {
		t.Fatalf("манифест: %v", err)
	}
	return man
}

// TestManifestChangesOnGeometry — миллиметр в геометрии меняет хеш.
//
// Решётка снята с обеих карт: её run покрывает ребро от края до края, и правка
// длины ребра потребовала бы править ещё и спан run'а. Тогда изменилось бы два
// поля, и тест перестал бы говорить именно про геометрию.
func TestManifestChangesOnGeometry(t *testing.T) {
	base := seedmap.Line(seedmap.WithoutConstruction())
	changed := seedmap.Line(seedmap.WithoutConstruction(), seedmap.Mutate(func(m *mapfmt.Map) {
		a := m.Geometry.Edges[seedmap.LineEdgeID]
		a.Horizontal[0].Length = seedmap.LineLengthM + 0.001
		m.Geometry.Edges[seedmap.LineEdgeID] = a
	}))
	if manifestOf(t, годная(t, base)).TrackHash == manifestOf(t, годная(t, changed)).TrackHash {
		t.Fatal("правка геометрии на миллиметр не изменила хеш")
	}
}

// TestTrackHashCoversElementKind — вид пути обязан входить в track_hash.
//
// # Зачем этот тест существует отдельно
//
// network_hash считается от САМИХ БАЙТОВ ответа: поле, попавшее в провод,
// попадает в хеш само, забыть его нельзя. track_hash считается от РУКОПИСНОЙ
// выжимки в writeTrackModel, и туда каждое новое поле модели надо вписать
// рукой. Ровно так проекту однажды обошёлся Slope: тело менялось, хеш нет, и
// клиент с immutable навсегда сохранял устаревшую геометрию — без единого
// отказа. Вывод, записанный в hash.go, дословен: «забудут».
//
// Тест — замок на этот случай: убери e.Kind из строки el| в writeTrackModel, и
// он упадёт.
//
// # Почему карта портится в обход валидатора
//
// Compile валидатором не пользуется, и это здесь нужно: сегодня валидатор
// принимает единственный вид, поэтому ДВУХ законных карт, различающихся только
// видом, не существует. Предмет теста — покрытие выжимки, а не перечень
// значений; в день, когда "road" станет законным, тест продолжит проверять то же
// самое, не изменившись.
func TestTrackHashCoversElementKind(t *testing.T) {
	случаи := map[string]func(*mapfmt.Map){
		// Ребро: вид пишет автор.
		"вид ребра": func(m *mapfmt.Map) { m.Topology.Edges[0].Kind = "road" },
		// Стрелка: её проходы — тоже элементы, и вид они берут у неё. Без этой
		// половины подмена вида устройства прошла бы мимо хеша.
		"вид стрелки": func(m *mapfmt.Map) { m.Topology.Turnouts[0].Kind = "road" },
	}
	base := manifestOf(t, seedmap.Station()).TrackHash
	for имя, порча := range случаи {
		t.Run(имя, func(t *testing.T) {
			if h := manifestOf(t, seedmap.Station(seedmap.Mutate(порча))).TrackHash; h == base {
				t.Fatalf("смена вида (%s) не изменила track_hash — выжимка описывает не всю модель пути", имя)
			}
		})
	}
}

// withGeoref добавляет карте валидную геопривязку: хеш должен зависеть от
// привязки (она меняет смысл координат), но не от provenance — правка
// комментария автора не должна сбрасывать кэш клиента.
func withGeoref(prov bool) seedmap.Option {
	return seedmap.Mutate(func(m *mapfmt.Map) {
		m.Georeference = &mapfmt.Georeference{
			Datum:            "WGS84",
			Origin:           mapfmt.Origin{Lat: 55.75, Lon: 37.62, H: 150.0},
			OriginHeightKind: "ellipsoidal",
			XAxisAzimuthDeg:  0,
			GroundToGrid:     1.0002,
		}
		if prov {
			m.Georeference.Provenance = map[string]string{"author": "тест", "note": "правка"}
		}
	})
}

func TestManifestIgnoresProvenance(t *testing.T) {
	if manifestOf(t, годная(t, seedmap.Line(withGeoref(false)))).TrackHash !=
		manifestOf(t, годная(t, seedmap.Line(withGeoref(true)))).TrackHash {
		t.Fatal("правка provenance изменила хеш")
	}
}

func TestManifestChangesOnGeoreference(t *testing.T) {
	if manifestOf(t, seedmap.Line()).TrackHash == manifestOf(t, годная(t, seedmap.Line(withGeoref(false)))).TrackHash {
		t.Fatal("правка геопривязки не изменила хеш")
	}
}

// TestRenderHashCoversEveryWireField — ETag обязан описывать ВСЁ отдаваемое тело.
//
// Первая редакция хешировала рукописную текстовую выжимку, в которую не входил
// Slope, хотя он сериализуется в JSON. Тело менялось, хеш — нет, и клиент с
// Cache-Control: immutable получал 304 и навсегда сохранял устаревшую геометрию.
// Отказа при этом не было: сервер стартовал, всё выглядело исправным.
//
// Тест перебирает поля провода и требует, чтобы правка каждого меняла хеш.
func TestRenderHashCoversEveryWireField(t *testing.T) {
	base := func() *RenderGeometry {
		return &RenderGeometry{
			Region: "X", Revision: 1,
			Elements: []RenderElement{{
				ID:    "E1",
				Kind:  mapfmt.KindRail,
				Start: PortPose{Plan: geom.Pose{X: 1, Y: 2, Heading: 0.3}, Z: 4, Slope: 0.005},
				Prims: []RenderPrimitive{{Kind: "arc", LengthM: 10, Radius: 300, Angle: 0.11}},
				Role:  &RenderRole{Turnout: "SW1", Branch: "diverging", Hand: "right", Frog: "1/9"},
			}},
			Trackside: []RenderTrackside{{
				ID: "TSP", Kind: "platform", Side: "right",
				Spans: []netloc.IntervalU{{Element: "E1", From: 10, To: 150}},
			}},
			TrackTypes: []RenderTrackType{{
				ID: "TRACK_MAIN", Gauge: 1.435,
				Sleeper: RenderSleeper{Pitch: 0.6, Length: 2.5, Width: 0.28},
				Ballast: RenderBallast{HalfWidth: 1.75},
			}},
			ConstructionRuns: []RenderRun{{
				ID: "RUN_1", Type: "TRACK_MAIN", Coordinate: "u", Phase: 0.15,
				Spans: []netloc.IntervalU{{Element: "E1", From: 0, To: 100, Direction: "forward"}},
			}},
			Features: []RenderFeature{{
				Owner: "SW1", Kind: "frog",
				Point: RenderPoint{X: 29.34, Y: -0.72},
				Addresses: []RenderAddress{{
					Element: "SW1:straight", U: 29.3,
					Tangent: RenderVec{X: 1, Y: 0},
				}},
			}},
			PlacementAlgorithm: "placement-v1",
		}
	}
	h0 := renderHashOf(t, base())

	mutations := map[string]func(*RenderGeometry){
		"region":                func(g *RenderGeometry) { g.Region = "Y" },
		"revision":              func(g *RenderGeometry) { g.Revision = 2 },
		"element id":            func(g *RenderGeometry) { g.Elements[0].ID = "E2" },
		"element kind":          func(g *RenderGeometry) { g.Elements[0].Kind = "road" },
		"start.plan.x":          func(g *RenderGeometry) { g.Elements[0].Start.Plan.X = 9 },
		"start.plan.y":          func(g *RenderGeometry) { g.Elements[0].Start.Plan.Y = 9 },
		"start.plan.heading":    func(g *RenderGeometry) { g.Elements[0].Start.Plan.Heading = 0.9 },
		"start.z":               func(g *RenderGeometry) { g.Elements[0].Start.Z = 9 },
		"start.slope":           func(g *RenderGeometry) { g.Elements[0].Start.Slope = 0.04 },
		"primitive kind":        func(g *RenderGeometry) { g.Elements[0].Prims[0].Kind = "straight" },
		"primitive length":      func(g *RenderGeometry) { g.Elements[0].Prims[0].LengthM = 11 },
		"primitive radius":      func(g *RenderGeometry) { g.Elements[0].Prims[0].Radius = 400 },
		"primitive angle":       func(g *RenderGeometry) { g.Elements[0].Prims[0].Angle = -0.11 },
		"добавлен element":      func(g *RenderGeometry) { g.Elements = append(g.Elements, g.Elements[0]) },
		"добавлен primitive":    func(g *RenderGeometry) { g.Elements[0].Prims = append(g.Elements[0].Prims, g.Elements[0].Prims[0]) },
		"role.turnout":          func(g *RenderGeometry) { g.Elements[0].Role.Turnout = "SW2" },
		"role.branch":           func(g *RenderGeometry) { g.Elements[0].Role.Branch = "straight" },
		"role.hand":             func(g *RenderGeometry) { g.Elements[0].Role.Hand = "left" },
		"role.frog":             func(g *RenderGeometry) { g.Elements[0].Role.Frog = "1/7" },
		"роль снята":            func(g *RenderGeometry) { g.Elements[0].Role = nil },
		"trackside id":          func(g *RenderGeometry) { g.Trackside[0].ID = "TSP2" },
		"trackside kind":        func(g *RenderGeometry) { g.Trackside[0].Kind = "buffer_stop" },
		"trackside side":        func(g *RenderGeometry) { g.Trackside[0].Side = "left" },
		"span element":          func(g *RenderGeometry) { g.Trackside[0].Spans[0].Element = "E2" },
		"span from":             func(g *RenderGeometry) { g.Trackside[0].Spans[0].From = 11 },
		"span to":               func(g *RenderGeometry) { g.Trackside[0].Spans[0].To = 151 },
		"добавлен span":         func(g *RenderGeometry) { g.Trackside[0].Spans = append(g.Trackside[0].Spans, g.Trackside[0].Spans[0]) },
		"тип id":                func(g *RenderGeometry) { g.TrackTypes[0].ID = "TRACK_SIDING" },
		"тип gauge":             func(g *RenderGeometry) { g.TrackTypes[0].Gauge = 1.520 },
		"тип pitch":             func(g *RenderGeometry) { g.TrackTypes[0].Sleeper.Pitch = 0.7 },
		"тип sleeper.length":    func(g *RenderGeometry) { g.TrackTypes[0].Sleeper.Length = 2.6 },
		"тип sleeper.width":     func(g *RenderGeometry) { g.TrackTypes[0].Sleeper.Width = 0.29 },
		"тип ballast":           func(g *RenderGeometry) { g.TrackTypes[0].Ballast.HalfWidth = 1.8 },
		"добавлен тип":          func(g *RenderGeometry) { g.TrackTypes = append(g.TrackTypes, g.TrackTypes[0]) },
		"run id":                func(g *RenderGeometry) { g.ConstructionRuns[0].ID = "RUN_2" },
		"run type":              func(g *RenderGeometry) { g.ConstructionRuns[0].Type = "TRACK_SIDING" },
		"run coordinate":        func(g *RenderGeometry) { g.ConstructionRuns[0].Coordinate = "s" },
		"run phase":             func(g *RenderGeometry) { g.ConstructionRuns[0].Phase = 0.3 },
		"run span element":      func(g *RenderGeometry) { g.ConstructionRuns[0].Spans[0].Element = "E2" },
		"run span from":         func(g *RenderGeometry) { g.ConstructionRuns[0].Spans[0].From = 1 },
		"run span to":           func(g *RenderGeometry) { g.ConstructionRuns[0].Spans[0].To = 99 },
		"run span direction":    func(g *RenderGeometry) { g.ConstructionRuns[0].Spans[0].Direction = "reverse" },
		"добавлен run":          func(g *RenderGeometry) { g.ConstructionRuns = append(g.ConstructionRuns, g.ConstructionRuns[0]) },
		"feature owner":         func(g *RenderGeometry) { g.Features[0].Owner = "SW2" },
		"feature kind":          func(g *RenderGeometry) { g.Features[0].Kind = "type_seam" },
		"feature point.x":       func(g *RenderGeometry) { g.Features[0].Point.X = 30 },
		"feature point.y":       func(g *RenderGeometry) { g.Features[0].Point.Y = -1 },
		"feature адрес element": func(g *RenderGeometry) { g.Features[0].Addresses[0].Element = "SW1:diverging" },
		"feature адрес u":       func(g *RenderGeometry) { g.Features[0].Addresses[0].U = 30 },
		"feature адрес tangent": func(g *RenderGeometry) { g.Features[0].Addresses[0].Tangent.X = 0.99 },
		"добавлен feature":      func(g *RenderGeometry) { g.Features = append(g.Features, g.Features[0]) },
		"placement_algorithm":   func(g *RenderGeometry) { g.PlacementAlgorithm = "placement-v2" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			g := base()
			mutate(g)
			if h := renderHashOf(t, g); h == h0 {
				t.Fatalf("правка поля %q не изменила network_hash — ETag описывает не всё тело", name)
			}
		})
	}
}

// renderHashOf берёт хеш ровно тех байт, которые уедут клиенту.
func renderHashOf(t *testing.T, rg *RenderGeometry) string {
	t.Helper()
	b, err := RenderBody(rg)
	if err != nil {
		t.Fatalf("сериализация: %v", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestManifestHashIsBodyHash — BuildManifest обязан брать хеш тех же байт.
// Без этой связки хеш и ответ снова разъедутся, просто чуть позже.
func TestManifestHashIsBodyHash(t *testing.T) {
	m := seedmap.Line()
	ct, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	man, err := BuildManifest(m, ct, rg)
	if err != nil {
		t.Fatalf("манифест: %v", err)
	}
	if man.NetworkHash != renderHashOf(t, rg) {
		t.Fatal("network_hash не является хешем отдаваемого тела")
	}
}
