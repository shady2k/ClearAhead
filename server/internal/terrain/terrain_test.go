package terrain

import (
	"bytes"
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/track"
	"github.com/shady2k/ClearAhead/server/internal/vegetation"
)

// Карта строится фабрикой, а не читается файлом: тест не должен зависеть от
// боевых данных и ломаться от их правки.
func loadMap(t *testing.T) *mapfmt.Map {
	t.Helper()
	m := seedmap.Station(seedmap.WithTerrain())
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("фикстура невалидна: %v", err)
	}
	return m
}

func buildField(t *testing.T, m *mapfmt.Map) (*Field, map[string]track.Element) {
	t.Helper()
	_, els, err := track.Propagate(m)
	if err != nil {
		t.Fatalf("распространение поз: %v", err)
	}
	f, err := New(m, els)
	if err != nil {
		t.Fatalf("построение рельефа: %v", err)
	}
	return f, els
}

// Под осью земля лежит НЕ на отметке оси, а на высоту конструкции ниже, и это
// главное следствие датума z.
//
// # Что здесь изменилось 2026-08-12 и почему это была ошибка, а не выбор
//
// Тест звался TestGroundUnderAxisSitsAtTrackElevation и требовал совпадения
// земли с отметкой оси. Требование было верным ровно до тех пор, пока не было
// сказано, ЧТО ТАКОЕ отметка оси. Контракт отрисовки редакции 6 §2 назвал её
// поверхностью катания — верхом головки рельса, — и совпадение земли с ней
// стало означать, что балласт, шпала и рельс занимают нулевую высоту.
//
// Цена прежней трактовки посчитана, а не оценена: на затравке ST_A земля
// строилась на 0.68 м выше должного (0.30 балласт + 0.20 шпала + 0.18 рельс).
// Бида ClearAhead-s6v оценивала «примерно полметра» — оценка была занижена на
// треть, и это записано затем, чтобы её не приняли задним числом за замер.
//
// Выборка идёт с nil-раскладкой НАМЕРЕННО: так p.Z остаётся отметкой ГОЛОВКИ
// РЕЛЬСА, и разность с землёй видна числом. Передай сюда настоящие спаны —
// тест снова стал бы проверять равенство и молча прошёл бы при нулевой
// поправке, то есть перестал бы ловить ровно ту ошибку, ради которой написан.
func TestGroundSitsBelowRailheadByTrackStructure(t *testing.T) {
	m := loadMap(t)
	f, els := buildField(t, m)

	// Ожидаемая высота конструкции берётся из карты, а не пишется числом:
	// правка затравки обязана менять ожидание вместе с фактом. Но ОДНО число
	// названо явно ниже — иначе тест согласится с любой поправкой, включая
	// нулевую.
	typ := m.Construction.Types[0]
	wantDrop := typ.FormationToRailTop()
	if math.Abs(wantDrop-0.68) > 1e-9 {
		t.Fatalf("высота конструкции затравки %.6f, ожидалось 0.68 — поменялись числа стека", wantDrop)
	}

	checked := 0
	for id, e := range els {
		pts, err := sampleAxis(e, nil, nil)
		if err != nil {
			t.Fatalf("%s: выборка оси: %v", id, err)
		}
		for _, p := range pts {
			got := f.WorkedM(p.X, p.Y)
			want := p.Z - wantDrop
			if math.Abs(got-want) > 1e-9 {
				t.Fatalf("%s: под осью в (%.3f, %.3f) земля на %.6f, ожидалось %.6f (головка рельса %.6f минус конструкция %.6f)",
					id, p.X, p.Y, got, want, p.Z, wantDrop)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("не проверено ни одной точки")
	}
	t.Logf("проверено точек оси: %d", checked)
}

// РЕГРЕССИЯ ClearAhead-27n: ось на нуле при базе рельефа 140.
//
// Земляные работы не спрашивают, разумна ли отметка оси. Правило «под основной
// площадкой земля лежит на отметке пути» они исполняют честно, и ось,
// потерявшая свою отметку при миграции JSON -> код, дала вдоль всего пути
// траншею в 140 м с почти отвесной стеной там, где кончается досягаемость
// откоса. Ни один тест этого не заметил, и не случайно: каждый сверял ЗЕМЛЮ С
// ОСЬЮ, а они были согласны друг с другом — расходились ось и рецепт.
//
// Проверка стоит здесь, а не в затравке, потому что расхождение — свойство
// ПАРЫ «ось + рецепт рельефа», и ловить его надо там, где обе величины
// впервые встречаются: тогда оно поймается у любой карты, а не только у той, у
// которой число сегодня заметили.
//
// Мера — полный размах шума рецепта: полоса, в которой природная земля вообще
// бывает. Отметка оси обязана лежать внутри неё. Насыпь и выемка внутри полосы
// — проект; ось, вынесенная за полосу целиком, — опечатка в отметке, потому что
// земляные работы тогда не примиряют поверхность с путём, а срывают её. Порог
// взят из самого рецепта, а не числом: карта с другим рельефом не потребует
// править тест.
//
// Мост и тоннель порог не задевают: там ось землю не тянет, и sampleAxis такие
// участки в выборку не берёт вовсе.
func TestAxisElevationAgreesWithTerrainBase(t *testing.T) {
	maps := map[string]*mapfmt.Map{
		"перегон": seedmap.Line(seedmap.WithTerrain()),
		"станция": seedmap.Station(seedmap.WithTerrain()),
		"перегон из двух рёбер": seedmap.Corridor(seedmap.WithTerrain()),
	}
	for name, m := range maps {
		t.Run(name, func(t *testing.T) {
			f, els := buildField(t, m)

			amplitude := 0.0
			for _, o := range m.Terrain.Octaves {
				amplitude += o.AmplitudeM
			}
			if amplitude <= 0 {
				t.Fatal("рецепт без амплитуд: порогу неоткуда взяться")
			}

			worstOffset, worstDepth := 0.0, 0.0
			var worstPoint axisPoint
			checked := 0
			for id, e := range els {
				pts, err := sampleAxis(e, nil, nil)
				if err != nil {
					t.Fatalf("%s: выборка оси: %v", id, err)
				}
				for _, p := range pts {
					offset := math.Abs(p.Z - f.BaseZ())
					if offset > worstOffset {
						worstOffset, worstPoint = offset, p
						worstDepth = math.Abs(p.Z - f.NaturalM(p.X, p.Y))
					}
					checked++
				}
			}
			if checked == 0 {
				t.Fatal("не проверено ни одной точки оси")
			}
			if worstOffset > amplitude {
				t.Fatalf("ось в (%.1f, %.1f) на отметке %.2f, база рельефа %.2f: "+
					"отступ %.2f м при размахе рельефа %.2f м; "+
					"земляные работы роют здесь %.2f м",
					worstPoint.X, worstPoint.Y, worstPoint.Z, f.BaseZ(),
					worstOffset, amplitude, worstDepth)
			}
			t.Logf("точек оси %d, худший отступ от базы %.2f м при размахе %.2f м",
				checked, worstOffset, amplitude)
		})
	}
}

// Вдали от пути земля природная: земляные работы не должны доставать до
// горизонта.
func TestGroundFarFromTrackIsNatural(t *testing.T) {
	m := loadMap(t)
	f, _ := buildField(t, m)

	x, y := 50000.0, 50000.0
	if got, want := f.WorkedM(x, y), f.NaturalM(x, y); got != want {
		t.Fatalf("вдали от пути земля %v, природная %v", got, want)
	}
}

// Рецепт с одной затравкой даёт один рельеф; с разной — разный. Без первого
// карта не воспроизводима, без второго затравка не работает.
func TestSeedDeterminesTerrain(t *testing.T) {
	m := loadMap(t)
	f1, _ := buildField(t, m)

	a := f1.NaturalM(1234.5, -678.9)
	b := f1.NaturalM(1234.5, -678.9)
	if a != b {
		t.Fatalf("один вызов дал %v, другой %v", a, b)
	}

	m2 := loadMap(t)
	m2.Terrain.Seed++
	f2, _ := buildField(t, m2)
	if f2.NaturalM(1234.5, -678.9) == a {
		t.Fatal("смена затравки не изменила рельеф")
	}
}

// Квантование — не украшение, а условие переносимости между машинами: даже при
// расхождении в последних битах сантиметр совпадёт. Проверяем, что округление
// происходит ровно один раз и от рабочей высоты.
func TestQuantizationToCentimeters(t *testing.T) {
	m := loadMap(t)
	f, _ := buildField(t, m)

	for _, p := range [][2]float64{{0, 0}, {150.25, -3.5}, {-800, 900}} {
		cm, err := f.HeightCm(p[0], p[1])
		if err != nil {
			t.Fatalf("(%v, %v): %v", p[0], p[1], err)
		}
		want := int16(math.Round((f.WorkedM(p[0], p[1]) - m.Terrain.BaseZ) * 100))
		if cm != want {
			t.Fatalf("(%v, %v): отсчёт %d, ожидалось %d", p[0], p[1], cm, want)
		}
	}
}

// МОСТ И ТОННЕЛЬ. На их протяжении путь несёт сооружение, а земля остаётся
// природной: без этого земляные работы сравняли бы долину под мостом и
// прокопали траншею над тоннелем — авторитет пути применился бы там, где путь
// земли не касается.
//
// Карта здесь одноэлементная намеренно. На станции соседние пути тянут землю к
// себе, и на её геометрии проверка вышла бы неубедительной: земля сдвинулась
// бы, но не до природной. Изолированный перегон даёт однозначный ответ.
func TestGroundUnderBridgeStaysNatural(t *testing.T) {
	for _, kind := range []string{"bridge", "tunnel"} {
		t.Run(kind, func(t *testing.T) {
			m := singleEdge(t, nil)
			withoutStructure, els := buildField(t, m)

			e := els[seedmap.LineEdgeID]
			pts, err := sampleAxis(e, nil, nil)
			if err != nil {
				t.Fatalf("выборка оси: %v", err)
			}
			p := pts[len(pts)/2]

			// Контроль: без сооружения земля притянута к оси — на высоту
			// конструкции ниже поверхности катания (см.
			// TestGroundSitsBelowRailheadByTrackStructure о том, почему не
			// вровень).
			wantAxis := p.Z - m.Construction.Types[0].FormationToRailTop()
			if !nearlyEqual(withoutStructure.WorkedM(p.X, p.Y), wantAxis) {
				t.Fatalf("без сооружения земля %v, ожидалось %v (ось %v)",
					withoutStructure.WorkedM(p.X, p.Y), wantAxis, p.Z)
			}
			// И она заметно отличается от природной — иначе проверка ниже
			// прошла бы сама собой на плоском рельефе.
			if nearlyEqual(withoutStructure.WorkedM(p.X, p.Y), withoutStructure.NaturalM(p.X, p.Y)) {
				t.Fatal("природная земля совпала с осью — тест ничего не докажет")
			}

			withStructure, _ := buildField(t, singleEdge(t, &mapfmt.Structure{
				// UUID из таблицы несущих сооружений seedmap (MOST/TONNEL):
				// тождество не выдумывается, метка — прежняя читаемая строка.
				ID:   "018bcfe5-683b-7242-8242-00003b424242",
				Name: "SOORUZHENIE",
				Kind: kind,
				Span: netloc.LinearU{{Element: seedmap.LineEdgeID, From: 0, To: seedmap.LineLengthM}},
			}))

			got := withStructure.WorkedM(p.X, p.Y)
			want := withStructure.NaturalM(p.X, p.Y)
			if !nearlyEqual(got, want) {
				t.Fatalf("%s: земля %v, ожидалась природная %v", kind, got, want)
			}
		})
	}
}

// singleEdge — минимальный перегон с рельефом, при необходимости несомый
// сооружением.
func singleEdge(t *testing.T, st *mapfmt.Structure) *mapfmt.Map {
	t.Helper()
	opts := []seedmap.Option{seedmap.WithTerrain()}
	if st != nil {
		opts = append(opts, seedmap.WithStructure(*st))
	}
	m := seedmap.Line(opts...)
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("минимальная карта отвергнута: %v", err)
	}
	return m
}

func nearlyEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// findForestChunk — первый чанк уровня 0 с лесом: покров, лес рецепта.
//
// Ищет перебором по окрестности оси: чанк нужен любой, лишь бы лес в нём был,
// иначе проверкам нечего срубить. Возвращает также пару ячеек с деревьями,
// чтобы тесту не догадываться, где лес.
func findForestChunk(t *testing.T, f *Field) (chunk.Address, []byte, []byte, [][2]int) {
	t.Helper()
	for cz := -3; cz <= 3; cz++ {
		for cx := -3; cx <= 3; cx++ {
			a := chunk.Address{Level: chunk.ForestLevel, CX: cx, CZ: cz}
			cover, err := f.ChunkCover(a)
			if err != nil {
				t.Fatalf("покров чанка (%d,%d): %v", cx, cz, err)
			}
			if cover == nil {
				continue
			}
			recipe := f.ChunkForest(a, cover)
			var trees [][2]int
			for j := range chunk.CoverCells {
				for i := range chunk.CoverCells {
					if chunk.ForestOccupied(recipe, i, j) {
						trees = append(trees, [2]int{i, j})
					}
				}
			}
			if len(trees) > 0 {
				return a, cover, recipe, trees
			}
		}
	}
	t.Fatal("леса в окрестности оси не нашлось ни в одном чанке уровня 0")
	return chunk.Address{}, nil, nil, nil
}

// Лес рецепта — чистая функция адреса: два прохода дают байт в байт один блоб,
// биты стоят только в ячейках лесного класса, и выше уровня 0 леса нет. Это
// фундамент проекции: компилятор растительности вычитает и складывает, и если
// рецепт недетерминирован, «срубленное не воскресает» недоказуемо.
func TestChunkForestRecipeIsStableAndClassPure(t *testing.T) {
	f, _ := buildField(t, loadMap(t))
	a, cover, recipe, _ := findForestChunk(t, f)
	again := f.ChunkForest(a, cover)
	if !bytes.Equal(recipe, again) {
		t.Fatal("два прохода ChunkForest дали разные блобы — рецепт недетерминирован")
	}
	for j := range chunk.CoverCells {
		for i := range chunk.CoverCells {
			if !chunk.ForestOccupied(recipe, i, j) {
				continue
			}
			class, _ := chunk.UnpackCover(cover[chunk.CoverIndex(i, j)])
			if class != chunk.SurfaceForestConifer && class != chunk.SurfaceForestBroad {
				t.Fatalf("бит в ячейке (%d,%d) класса %d — не лес", i, j, class)
			}
		}
	}
	if got := f.ChunkForest(chunk.Address{Level: 1, CX: a.CX, CZ: a.CZ}, cover); got != nil {
		t.Fatalf("лес на уровне 1 дал %d байт, ожидался nil", len(got))
	}
}

// ПРИЁМОЧНЫЙ КРИТЕРИЙ W2-B на настоящем рельефе: срубленное дерево не
// воскресает после пересборки проекции из рецепта, а уцелевшие не сдвинулись —
// проекция отличается от рецепта ровно одной ячейкой.
func TestProjectionKeepsCutTreeCutOnRealField(t *testing.T) {
	f, _ := buildField(t, loadMap(t))
	a, cover, recipe, trees := findForestChunk(t, f)
	cut := trees[0]
	srcs := vegetation.Sources{Cuts: []vegetation.Cut{{CX: a.CX, CZ: a.CZ, I: cut[0], J: cut[1]}}}
	first, err := vegetation.Project(a, recipe, cover, srcs)
	if err != nil {
		t.Fatalf("проекция: %v", err)
	}
	if chunk.ForestOccupied(first, cut[0], cut[1]) {
		t.Fatal("срубленное дерево осталось в проекции")
	}
	second, err := vegetation.Project(a, recipe, cover, srcs)
	if err != nil {
		t.Fatalf("пересборка: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("пересборка из рецепта изменила проекцию")
	}
	diff := 0
	for j := range chunk.CoverCells {
		for i := range chunk.CoverCells {
			if chunk.ForestOccupied(first, i, j) != chunk.ForestOccupied(recipe, i, j) {
				diff++
			}
		}
	}
	if diff != 1 {
		t.Fatalf("проекция отличается от рецепта в %d ячейках, ожидалась 1 — соседи сдвинулись", diff)
	}
}
