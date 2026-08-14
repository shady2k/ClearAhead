package mapfmt

import (
	"fmt"
	"math"
)

// Лимиты рельефа. Октав немного по существу: каждая следующая вдвое мельче, и
// восьми хватает на диапазон от километра до метра.
const (
	MaxTerrainOctaves = 8
	// MaxTerrainAmplitudeM — суммарный размах шума. Ограничен так, чтобы
	// отсчёты гарантированно помещались в int16 сантиметров относительно
	// base_z (±327,67 м) с запасом на земляные работы.
	MaxTerrainAmplitudeM = 250.0
)

// validateTerrain проверяет рецепт рельефа. Отказывает, не чинит.
func validateTerrain(t *Terrain) error {
	if t == nil {
		// Карта без рельефа законна: отсчётов просто нет.
		return nil
	}
	if err := checkFiniteFloat("рельеф: base_z", t.BaseZ); err != nil {
		return err
	}
	if len(t.Octaves) == 0 {
		return fmt.Errorf("mapfmt: рельеф: нет ни одной октавы; карта без рельефа записывается отсутствием блока, а не пустым блоком")
	}
	if len(t.Octaves) > MaxTerrainOctaves {
		return fmt.Errorf("mapfmt: рельеф: октав больше %d", MaxTerrainOctaves)
	}
	total := 0.0
	prev := math.Inf(1)
	for i, o := range t.Octaves {
		if err := checkFiniteFloat(fmt.Sprintf("рельеф: октава %d: длина волны", i), o.WavelengthM); err != nil {
			return err
		}
		if err := checkFiniteFloat(fmt.Sprintf("рельеф: октава %d: размах", i), o.AmplitudeM); err != nil {
			return err
		}
		if !(o.WavelengthM > 0) {
			return fmt.Errorf("mapfmt: рельеф: октава %d: длина волны должна быть положительной, получено %v", i, o.WavelengthM)
		}
		if !(o.AmplitudeM >= 0) {
			return fmt.Errorf("mapfmt: рельеф: октава %d: размах должен быть неотрицательным, получено %v", i, o.AmplitudeM)
		}
		// Порядок от крупного к мелкому — не вкусовщина: сумма считается в
		// записанном порядке, и требование порядка делает рецепт канонической
		// записью одного и того же рельефа, а не одной из перестановок.
		if o.WavelengthM >= prev {
			return fmt.Errorf("mapfmt: рельеф: октава %d: длина волны %v не меньше предыдущей %v — октавы записываются от крупной к мелкой",
				i, o.WavelengthM, prev)
		}
		prev = o.WavelengthM
		total += o.AmplitudeM
	}
	if err := validateCover(t.Cover); err != nil {
		return err
	}
	if total > MaxTerrainAmplitudeM {
		return fmt.Errorf("mapfmt: рельеф: суммарный размах %v м больше %v — отсчёты перестанут помещаться в целые сантиметры относительно base_z",
			total, MaxTerrainAmplitudeM)
	}

	e := t.Earthworks
	if err := checkFiniteFloat("рельеф: полуширина площадки", e.FormationHalfWidth); err != nil {
		return err
	}
	if err := checkFiniteFloat("рельеф: заложение откоса", e.SideSlope); err != nil {
		return err
	}
	if !(e.FormationHalfWidth > 0) {
		return fmt.Errorf("mapfmt: рельеф: полуширина основной площадки должна быть положительной, получено %v", e.FormationHalfWidth)
	}
	if !(e.SideSlope > 0) {
		return fmt.Errorf("mapfmt: рельеф: заложение откоса должно быть положительным, получено %v", e.SideSlope)
	}
	// Охват проверяется ПОСЛЕ земляных работ и размаха шума, потому что его
	// нижняя граница считается из них.
	if err := validateExtent(t.Extent, e.FormationHalfWidth+total*e.SideSlope); err != nil {
		return err
	}
	// Домен проверяется рядом с охватом: оба описывают пространство мира, и
	// две ошибки одного блока не должны разъезжаться по разным файлам.
	return validateDomain(t.Domain)
}

// Пределы охвата. Числа отвергают невозможное, а не выбирают размер: размер
// выбирает автор карты.
const (
	// MaxDetailLevels — потолок числа уровней. Сторона чанка есть 256 << level,
	// и на шестнадцатом уровне это больше диаметра Земли; дальше сдвиг теряет
	// смысл раньше, чем разрядность. Тот же потолок знает разбор адреса чанка
	// (chunk.MaxLevelLimit) — там он отвергает мусор в URL.
	MaxDetailLevels = 16
	// MaxReachM — потолок охвата. Регион объявлен размером 400 × 400 км
	// (`world-storage` §1), и мир, тянущийся дальше собственного региона,
	// означает не большую карту, а описку в числе уровней.
	//
	// Цена ошибки не гипотетическая: число клеток растёт как квадрат охвата, и
	// лишний уровень у станции — это учетверение обхода на порождении.
	MaxReachM = 400000.0
)

// validateExtent проверяет охват. Отказывает, не чинит.
//
// earthworksReachM — вылет откосов этой же карты: дальше него земляные работы
// не достают (terrain.Field.reach считает то же самое из тех же чисел).
func validateExtent(e Extent, earthworksReachM float64) error {
	if err := checkFiniteFloat("рельеф: охват: радиус уровня 0", e.Level0RadiusM); err != nil {
		return err
	}
	// Ноль уровней — это ЗАБЫТОЕ ПОЛЕ, а не выбор: JSON без ключа даёт тот же
	// ноль, что и явный ноль, и молча принять его значило бы выдать карту без
	// охвата за исправную. Поэтому счёт штуками и отказ на нуле.
	if !(e.Levels >= 1 && e.Levels <= MaxDetailLevels) {
		return fmt.Errorf("mapfmt: рельеф: охват: уровней %d вне [1, %d]; уровни считаются штуками, и пропущенное поле даёт ноль — блок extent обязателен",
			e.Levels, MaxDetailLevels)
	}
	if !(e.Level0RadiusM > 0) {
		return fmt.Errorf("mapfmt: рельеф: охват: радиус уровня 0 должен быть положительным, получено %v", e.Level0RadiusM)
	}
	// СОГЛАСОВАННОСТЬ ДВУХ ЧИСЕЛ КАРТЫ, а не диапазон. Уровень 0 существует
	// затем, чтобы земляные работы легли на мелкую сетку; радиус меньше вылета
	// откосов оставляет подошву насыпи на сетке вчетверо грубее и выглядит при
	// этом как исправная карта — просто насыпь у края коридора перестаёт
	// сходиться с землёй.
	if e.Level0RadiusM < earthworksReachM {
		return fmt.Errorf("mapfmt: рельеф: охват: радиус уровня 0 равен %v м, а откосы этой карты вылетают на %v м — уровень 0 обязан покрывать собственные земляные работы",
			e.Level0RadiusM, earthworksReachM)
	}
	if reach := e.ReachM(); reach > MaxReachM {
		return fmt.Errorf("mapfmt: рельеф: охват: радиус %v м · 2^%d = %v м больше %v м",
			e.Level0RadiusM, e.MaxLevel(), reach, MaxReachM)
	}
	return nil
}

// validateDomain проверяет прямоугольник домена. Отказывает, не чинит.
//
// Нуль здесь — ЗАБЫТАЯ СТРОКА, а не значение: JSON без ключа даёт тот же ноль,
// что и явный ноль, и молча принять его значило бы выдать карту без мира за
// исправную. Поэтому проверка — на вырожденность, а не на диапазон: домен,
// у которого сторона не выросла, не задан. Отказ называет ВСЕ ЧЕТЫРЕ числа —
// автор ищет пропуск глазами, и отсутствующее в тексте число он ищет по всей
// карте.
func validateDomain(d Domain) error {
	for _, c := range []struct {
		name string
		v    float64
	}{
		{"min_x", d.MinX}, {"min_z", d.MinZ}, {"max_x", d.MaxX}, {"max_z", d.MaxZ},
	} {
		if err := checkFiniteFloat("рельеф: домен: "+c.name, c.v); err != nil {
			return err
		}
	}
	if !(d.MinX < d.MaxX && d.MinZ < d.MaxZ) {
		return fmt.Errorf("mapfmt: рельеф: домен: прямоугольник не задан или вырожден: x от %v до %v, z от %v до %v; блок domain обязателен",
			d.MinX, d.MaxX, d.MinZ, d.MaxZ)
	}
	return nil
}

func checkFiniteFloat(what string, v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("mapfmt: %s: значение не конечно (%v)", what, v)
	}
	return nil
}

// Диапазоны рецепта покрова. Числа ПРЕДВАРИТЕЛЬНЫЕ и отвергают заведомо
// невозможное, а не проверяют замысел: источник не назначен, как и у профиля
// норм.
//
// Пороги проверяются диапазоном [-1, 1] потому, что маска — значение шума
// terrain.valueNoise, а он по построению лежит в этих границах. Порог вне их
// означает «леса нет никогда» либо «лес всюду»: и то и другое выразимо
// осмысленнее — отсутствием блока cover либо порогом на самой границе, — а
// молча принятый недостижимый порог выглядел бы как исправная карта с пустым
// лесом, и автор искал бы ошибку в другом месте.
const (
	MinCoverWavelengthM, MaxCoverWavelengthM = 1.0, 100000.0
	MaxCoverClearHalfWidthM                  = 1000.0
	// MaxCoverOctaves — потолок по той же причине, что у рельефа: каждая
	// следующая октава вдвое мельче, и за шестой мельчает мельче ячейки
	// покрова (4 м на уровне 0), то есть считается впустую.
	MaxCoverOctaves = 6
)

// validateCover проверяет рецепт покрова. Отказывает, не чинит.
func validateCover(c *Cover) error {
	if c == nil {
		// Карта без покрова законна: ресурс покрова просто не существует.
		return nil
	}
	wave := func(name string, v float64) error {
		if !(v >= MinCoverWavelengthM && v <= MaxCoverWavelengthM) {
			return fmt.Errorf("mapfmt: покров: %s %v вне [%v, %v] м", name, v, MinCoverWavelengthM, MaxCoverWavelengthM)
		}
		return nil
	}
	if err := wave("длина волны леса", c.ForestWavelengthM); err != nil {
		return err
	}
	if err := wave("длина волны породы", c.SpeciesWavelengthM); err != nil {
		return err
	}
	if err := wave("длина волны низового покрова", c.VegWavelengthM); err != nil {
		return err
	}
	// Октавы обязательны и не подставляются единицей. Ноль октав — это маска,
	// тождественно равная нулю: лес либо всюду, либо нигде, и покров ровно
	// такой же. Молчаливая единица выглядела бы исправной картой с материками
	// вместо мозаики — ровно та ошибка, которой этот проект уже заплатил.
	oct := func(name string, v int) error {
		if !(v >= 1 && v <= MaxCoverOctaves) {
			return fmt.Errorf("mapfmt: покров: октав %s %d вне [1, %d]", name, v, MaxCoverOctaves)
		}
		return nil
	}
	if err := oct("маски леса", c.ForestOctaves); err != nil {
		return err
	}
	if err := oct("маски породы", c.SpeciesOctaves); err != nil {
		return err
	}
	if err := oct("маски низового покрова", c.VegOctaves); err != nil {
		return err
	}
	thr := func(name string, v float64) error {
		if !(v >= -1 && v <= 1) {
			return fmt.Errorf("mapfmt: покров: %s %v вне [-1, 1] — маска есть значение шума и других значений не принимает", name, v)
		}
		return nil
	}
	if err := thr("порог леса", c.ForestThreshold); err != nil {
		return err
	}
	if err := thr("порог сомкнутого леса", c.ForestDenseThreshold); err != nil {
		return err
	}
	if err := thr("порог голой почвы", c.BareThreshold); err != nil {
		return err
	}
	if err := thr("порог сомкнутого покрова", c.ClosedThreshold); err != nil {
		return err
	}
	// Пороги ПАРНЫЕ, и порядок в паре — не придирка. Между ними величина растёт
	// от нуля до полной; равные пороги дают деление на ноль, перевёрнутые —
	// растущую наоборот величину. И то и другое выглядело бы исправной картой с
	// вывернутым покровом, и автор искал бы ошибку в рендерере.
	if !(c.ClosedThreshold > c.BareThreshold) {
		return fmt.Errorf("mapfmt: покров: порог сомкнутого покрова %v не выше порога голой почвы %v — между ними растёт сомкнутость, и расти ей некуда",
			c.ClosedThreshold, c.BareThreshold)
	}
	if !(c.ForestDenseThreshold > c.ForestThreshold) {
		return fmt.Errorf("mapfmt: покров: порог сомкнутого леса %v не выше порога леса %v — между ними растёт плотность посадки, и расти ей некуда",
			c.ForestDenseThreshold, c.ForestThreshold)
	}
	if !(c.ClearHalfWidthM >= 0 && c.ClearHalfWidthM <= MaxCoverClearHalfWidthM) {
		return fmt.Errorf("mapfmt: покров: полуширина отчуждения %v вне [0, %v] м", c.ClearHalfWidthM, MaxCoverClearHalfWidthM)
	}
	return nil
}

// Диапазоны построек. Числа предварительные и отвергают заведомо невозможное.
const (
	MinBuildingSizeM   = 2.0
	MaxBuildingSizeM   = 500.0
	MinBuildingHeightM = 2.0
	MaxBuildingHeightM = 200.0
	MaxBuildings       = 100000
)

// validateObjects проверяет семантические объекты региона. Отказывает, не чинит.
func (m *Map) validateObjects() error {
	o := m.Objects
	if o == nil {
		return nil
	}
	if len(o.Buildings) > MaxBuildings {
		return fmt.Errorf("mapfmt: объекты: построек больше %d", MaxBuildings)
	}
	seen := make(map[string]bool, len(o.Buildings))
	for i := range o.Buildings {
		b := &o.Buildings[i]
		if err := checkEntity("постройка", b.Name, b.ID); err != nil {
			return err
		}
		if seen[b.ID] {
			return fmt.Errorf("mapfmt: постройка %q объявлена дважды", Labeled(b.Name, b.ID))
		}
		seen[b.ID] = true
		if err := checkFiniteFloat(fmt.Sprintf("постройка %s: x", Labeled(b.Name, b.ID)), b.X); err != nil {
			return err
		}
		if err := checkFiniteFloat(fmt.Sprintf("постройка %s: y", Labeled(b.Name, b.ID)), b.Y); err != nil {
			return err
		}
		if err := checkFiniteFloat(fmt.Sprintf("постройка %s: heading", Labeled(b.Name, b.ID)), b.Heading); err != nil {
			return err
		}
		// Диапазоны, а не знак: дом шириной 0.001 м проходит «строго
		// положительно» и рисуется невидимой щепкой, а миллион таких кладёт
		// клиент.
		bad := func(what string, v, min, max float64) error {
			return fmt.Errorf("mapfmt: постройка %q: %s %g вне [%g, %g] м", Labeled(b.Name, b.ID), what, v, min, max)
		}
		if !(b.Width >= MinBuildingSizeM && b.Width <= MaxBuildingSizeM) {
			return bad("width", b.Width, MinBuildingSizeM, MaxBuildingSizeM)
		}
		if !(b.Depth >= MinBuildingSizeM && b.Depth <= MaxBuildingSizeM) {
			return bad("depth", b.Depth, MinBuildingSizeM, MaxBuildingSizeM)
		}
		if !(b.Height >= MinBuildingHeightM && b.Height <= MaxBuildingHeightM) {
			return bad("height", b.Height, MinBuildingHeightM, MaxBuildingHeightM)
		}
	}
	return validateRivers(o.Rivers)
}

// Диапазоны рек. Числа предварительные и отвергают заведомо невозможное, а не
// проверяют замысел: источник норм не назначен, как и у профиля.
const (
	MinRiverAxisPoints = 2
	MaxRiverAxisPoints = 100000
	MaxRiverHalfWidthM = 5000.0
	MaxRiverBankM      = 5000.0
	MaxRiverDepthM     = 500.0
	MaxRiverSandBandM  = 1000.0
	MaxRiverRimM       = 500.0
	MaxRiverValleyM    = 20000.0
)

// validateRivers проверяет реки. Отказывает, не чинит.
func validateRivers(rivers []River) error {
	seen := make(map[string]bool, len(rivers))
	for i := range rivers {
		r := &rivers[i]
		if err := checkEntity("река", r.Name, r.ID); err != nil {
			return err
		}
		if seen[r.ID] {
			return fmt.Errorf("mapfmt: река %q объявлена дважды", Labeled(r.Name, r.ID))
		}
		seen[r.ID] = true
		// Один точки мало не по формальности: русло — ЛИНЕЙНЫЙ объект, и река
		// из одной точки не имеет ни направления, ни длины. Врезать её в рельеф
		// значило бы выдавить круглую яму и назвать это рекой.
		if len(r.Axis) < MinRiverAxisPoints {
			return fmt.Errorf("mapfmt: река %q: точек оси %d, нужно не меньше %d — у русла есть направление",
				Labeled(r.Name, r.ID), len(r.Axis), MinRiverAxisPoints)
		}
		if len(r.Axis) > MaxRiverAxisPoints {
			return fmt.Errorf("mapfmt: река %q: точек оси больше %d", Labeled(r.Name, r.ID), MaxRiverAxisPoints)
		}
		for k, p := range r.Axis {
			for _, c := range []struct {
				name string
				v    float64
			}{{"x", p.X}, {"y", p.Y}, {"z", p.Z}} {
				if err := checkFiniteFloat(fmt.Sprintf("река %s: точка %d: %s", Labeled(r.Name, r.ID), k, c.name), c.v); err != nil {
					return err
				}
			}
		}
		bad := func(what string, v, min, max float64) error {
			return fmt.Errorf("mapfmt: река %q: %s %g вне [%g, %g] м", Labeled(r.Name, r.ID), what, v, min, max)
		}
		if !(r.HalfWidthM > 0 && r.HalfWidthM <= MaxRiverHalfWidthM) {
			return bad("half_width", r.HalfWidthM, 0, MaxRiverHalfWidthM)
		}
		// Берег шириной ноль законен: обрыв — тоже берег. Отрицательный — нет.
		if !(r.BankM >= 0 && r.BankM <= MaxRiverBankM) {
			return bad("bank", r.BankM, 0, MaxRiverBankM)
		}
		if !(r.DepthM > 0 && r.DepthM <= MaxRiverDepthM) {
			return bad("depth", r.DepthM, 0, MaxRiverDepthM)
		}
		if !(r.SandBandM >= 0 && r.SandBandM <= MaxRiverSandBandM) {
			return bad("sand_band", r.SandBandM, 0, MaxRiverSandBandM)
		}
		// Бровка строго выше уреза: равная означала бы реку, налитую вровень с
		// краями, и любая неровность шума выпускала бы воду в поле. Ноль здесь
		// не «обрыв», как у берега, а неработающая река.
		if !(r.RimM > 0 && r.RimM <= MaxRiverRimM) {
			return bad("rim", r.RimM, 0, MaxRiverRimM)
		}
		// Долина шириной ноль законна: у горной реки её и нет, берег сразу
		// упирается в склон. Отрицательной — нет.
		if !(r.ValleyM >= 0 && r.ValleyM <= MaxRiverValleyM) {
			return bad("valley", r.ValleyM, 0, MaxRiverValleyM)
		}
	}
	return nil
}
