// Команда gen_st_a_construction порождает блок construction карты ST_A.
//
// Требование плана волны 2a (задача 5): блок construction порождён скриптом, а
// не набран руками — по одному run на ребро с фазой 0, кроме пары ST_A_E_E34 и
// ST_A_E_T4, которые стыкуются напрямую в узле ST_A_T4_E и сливаются в один run.
//
// Скрипт идемпотентен: повторный прогон заменяет существующий блок
// construction, не трогая остальное тело файла (значения разбираются как
// RawMessage и сохраняют авторское написание чисел). Версии поднимаются только
// вверх: format_version до 2, map_revision минимум до 3 — повторный прогон
// после ручной правки ревизии не откатит её назад.
//
// Запуск: go run ./cmd/gen_st_a_construction [путь к maps/st_a.json]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// doc — документ карты с сохранением авторских байт значений: тело топологии и
// геометрии уезжает обратно как есть, меняются только версии и блок construction.
type doc struct {
	FormatVersion int             `json:"format_version"`
	MapID         string          `json:"map_id"`
	MapRevision   int             `json:"map_revision"`
	Georeference  json.RawMessage `json:"georeference,omitempty"`
	Anchors       json.RawMessage `json:"anchors"`
	Topology      json.RawMessage `json:"topology"`
	Geometry      json.RawMessage `json:"geometry"`
	Construction  json.RawMessage `json:"construction,omitempty"`
}

type edgeDecl struct {
	ID string `json:"id"`
}

type hprim struct {
	Kind   string  `json:"kind"`
	Length float64 `json:"length,omitempty"`
	Radius float64 `json:"radius,omitempty"`
	Angle  float64 `json:"angle,omitempty"`
}

type edgeGeometry struct {
	Horizontal []hprim `json:"horizontal"`
}

type construction struct {
	DefaultType string      `json:"default_type"`
	Types       []trackType `json:"types"`
	Runs        []trackRun  `json:"runs"`
}

type trackType struct {
	ID      string       `json:"id"`
	Gauge   float64      `json:"gauge"`
	Sleeper trackSleeper `json:"sleeper"`
	Ballast trackBallast `json:"ballast"`
}

type trackSleeper struct {
	Pitch  float64 `json:"pitch"`
	Length float64 `json:"length"`
	Width  float64 `json:"width"`
}

type trackBallast struct {
	HalfWidth float64 `json:"half_width"`
}

type trackRun struct {
	ID         string    `json:"id"`
	Coordinate string    `json:"coordinate"`
	Phase      float64   `json:"phase"`
	Spans      []runSpan `json:"spans"`
}

type runSpan struct {
	Element   string  `json:"element"`
	From      float64 `json:"from"`
	To        float64 `json:"to"`
	Direction string  `json:"direction"`
}

func main() {
	flag.Parse()
	path := "maps/st_a.json"
	if flag.NArg() > 0 {
		path = flag.Arg(0)
	}
	if err := run(path); err != nil {
		fmt.Fprintln(os.Stderr, "gen_st_a_construction:", err)
		os.Exit(1)
	}
}

func run(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var d doc
	if err := json.Unmarshal(raw, &d); err != nil {
		return fmt.Errorf("разбор %s: %w", path, err)
	}

	var topo struct {
		Edges []edgeDecl `json:"edges"`
	}
	if err := json.Unmarshal(d.Topology, &topo); err != nil {
		return fmt.Errorf("топология: %w", err)
	}
	var geoms struct {
		Edges map[string]edgeGeometry `json:"edges"`
	}
	if err := json.Unmarshal(d.Geometry, &geoms); err != nil {
		return fmt.Errorf("геометрия: %w", err)
	}
	length := func(id string) (float64, error) {
		g, ok := geoms.Edges[id]
		if !ok {
			return 0, fmt.Errorf("ребро %s без геометрии", id)
		}
		var u float64
		for i, p := range g.Horizontal {
			switch p.Kind {
			case "straight":
				u += p.Length
			case "arc":
				u += p.Radius * math.Abs(p.Angle)
			default:
				return 0, fmt.Errorf("ребро %s: примитив %d: kind %q", id, i, p.Kind)
			}
		}
		return u, nil
	}

	// По одному run на ребро, фаза 0, направление forward. Исключение — пара
	// ST_A_E_E34 и ST_A_E_T4: они стыкуются напрямую в узле ST_A_T4_E, run один,
	// и T4 в нём проходится в обратном направлении (от узла к стрелке).
	merged := map[string]bool{"ST_A_E_E34": true, "ST_A_E_T4": true}
	var runs []trackRun
	var edgeIDs []string
	for _, e := range topo.Edges {
		edgeIDs = append(edgeIDs, e.ID)
	}
	sort.Strings(edgeIDs)
	for _, id := range edgeIDs {
		if merged[id] {
			continue
		}
		l, err := length(id)
		if err != nil {
			return err
		}
		runs = append(runs, trackRun{
			ID:         "RUN_" + id,
			Coordinate: "u",
			Phase:      0,
			Spans: []runSpan{{
				Element:   id,
				From:      0,
				To:        l,
				Direction: "forward",
			}},
		})
	}
	l34, err := length("ST_A_E_E34")
	if err != nil {
		return err
	}
	l4, err := length("ST_A_E_T4")
	if err != nil {
		return err
	}
	runs = append(runs, trackRun{
		ID:         "RUN_ST_A_E_E34_T4",
		Coordinate: "u",
		Phase:      0,
		Spans: []runSpan{
			{Element: "ST_A_E_E34", From: 0, To: l34, Direction: "forward"},
			{Element: "ST_A_E_T4", From: 0, To: l4, Direction: "reverse"},
		},
	})

	// Один тип достаточно (план, задача 5): главная решётка 1435. Тип устройства
	// и типы run'ов в карте опущены — применяется default_type, компилятор
	// разрешает умолчание и в проводе ссылка всегда явная.
	blk := construction{
		DefaultType: "TRACK_MAIN_1435",
		Types: []trackType{{
			ID:    "TRACK_MAIN_1435",
			Gauge: 1.435,
			Sleeper: trackSleeper{
				Pitch:  0.6,
				Length: 2.5,
				Width:  0.28,
			},
			Ballast: trackBallast{HalfWidth: 1.75},
		}},
		Runs: runs,
	}
	blkJSON, err := json.MarshalIndent(blk, "", "  ")
	if err != nil {
		return err
	}
	d.Construction = blkJSON
	if d.FormatVersion < 2 {
		d.FormatVersion = 2
	}
	if d.MapRevision < 3 {
		d.MapRevision = 3
	}

	out, err := json.MarshalIndent(&d, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return err
	}

	// Самопроверка: записанный файл обязан пройти строгий разбор и валидацию.
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	m, err := mapfmt.Decode(f)
	if err != nil {
		return fmt.Errorf("самопроверка: разбор записанного файла: %w", err)
	}
	if err := mapfmt.Validate(m); err != nil {
		return fmt.Errorf("самопроверка: записанный файл не проходит валидацию: %w", err)
	}
	fmt.Printf("ok: %s: format_version %d, map_revision %d, run'ов %d\n",
		filepath.Clean(path), d.FormatVersion, d.MapRevision, len(runs))
	return nil
}
