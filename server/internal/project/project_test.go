package project

import (
	"strings"
	"testing"
)

// TestDeclaredComplete — у каждой объявленной проекции заполнены все пять полей
// (§5.4 спеки): входы, габарит влияния, уровни, формат, группа. Пропущенное
// поле ловится тестом, а не глазом.
func TestDeclaredComplete(t *testing.T) {
	if len(declared) == 0 {
		t.Fatal("таблица проекций пуста")
	}
	seen := make(map[Projection]bool)
	for i := range declared {
		d := &declared[i]
		if !d.Projection.Known() {
			t.Errorf("проекция %d вне объявленного ряда", int(d.Projection))
		}
		if seen[d.Projection] {
			t.Errorf("проекция %d объявлена дважды", int(d.Projection))
		}
		seen[d.Projection] = true
		if len(d.Inputs) == 0 {
			t.Errorf("проекция %d: входы не объявлены", int(d.Projection))
		}
		if d.Recipe == "" {
			t.Errorf("проекция %d: зависимость от рецепта не объявлена", int(d.Projection))
		}
		if d.Footprint == "" {
			t.Errorf("проекция %d: габарит влияния не объявлен", int(d.Projection))
		}
		if d.Format == "" {
			t.Errorf("проекция %d: формат результата не объявлен", int(d.Projection))
		}
		if !d.Group.Known() {
			t.Errorf("проекция %d: группа согласованности %d вне ряда", int(d.Projection), int(d.Group))
		}
		switch d.Levels {
		case levelsEvery, levelsForest, levelsWorld:
		default:
			t.Errorf("проекция %d: уровни %d вне ряда", int(d.Projection), int(d.Levels))
		}
		switch d.Cells {
		case cellsClosed, cellsOpen:
		default:
			t.Errorf("проекция %d: клетки %d вне ряда", int(d.Projection), int(d.Cells))
		}
	}
	for p := Network; p <= Collision; p++ {
		if !seen[p] {
			t.Errorf("проекция %d не объявлена", int(p))
		}
	}
}

// TestClosureProjectionsAddressable — проекции, которые Closure может выдать,
// все объявлены и не являются клиентскими производными. Производные клиента
// (маска балласта, коллизия) адресов не порождают — они пересобираются при
// получении своей входной проекции.
func TestClosureProjectionsAddressable(t *testing.T) {
	for _, p := range []Projection{Network, Surface, Cover, Vegetation, Water, Geometry} {
		d := declaration(p)
		if d == nil {
			t.Errorf("проекция %d не объявлена", int(p))
			continue
		}
		if d.ClientDerived {
			t.Errorf("проекция %d — клиентская производная, но Closure её выдаёт", int(p))
		}
	}
}

// TestClientDerivedDeclared — производные клиента объявлены честно: формат
// называет, что они строятся на клиенте, а группа совпадает с группой их
// входной проекции.
func TestClientDerivedDeclared(t *testing.T) {
	mask := declaration(BallastMask)
	if mask == nil || !mask.ClientDerived {
		t.Fatal("маска балласта обязана быть объявленной клиентской производной")
	}
	if mask.Group != GroupNetwork {
		t.Errorf("маска балласта строится из сети, группа %d, ожидалась %d", int(mask.Group), int(GroupNetwork))
	}
	col := declaration(Collision)
	if col == nil || !col.ClientDerived {
		t.Fatal("коллизия обязана быть объявленной клиентской производной")
	}
	if col.Group != GroupGeometry {
		t.Errorf("коллизия строится из геометрии, группа %d, ожидалась %d", int(col.Group), int(GroupGeometry))
	}
}

// TestSourceKindsKnown — объявленные виды известны, неизвестный — нет. Значение
// вне ряда обязано дать отказ в Closure, а не пустое замыкание.
func TestSourceKindsKnown(t *testing.T) {
	for k := SourcePath; k <= SourceClearing; k++ {
		if !k.Known() {
			t.Errorf("вид исходника %d объявлен, но Known() = false", int(k))
		}
	}
	if SourceKind(99).Known() {
		t.Error("SourceKind(99) объявлен известным — ряд оборван")
	}
}

// TestRiverReachFromRecipe — радиус охранной области берётся из рецепта реки:
// reach = HalfWidth + Bank + Valley, ровно та полоса, в которой carveRiver
// задаёт поверхность (terrain.go:616). Число проверяется, а не цитируется.
func TestRiverReachFromRecipe(t *testing.T) {
	r := River{ID: "река", HalfWidthM: 10, BankM: 5, ValleyM: 85}
	if got := r.ReachM(); got != 100 {
		t.Errorf("reach = %v, ожидалось 100", got)
	}
}

// TestDeclarationsDeepCopy — объявление, выданное наружу, не делит память с
// таблицей: правка копии не должна менять замыкание.
func TestDeclarationsDeepCopy(t *testing.T) {
	before := Declarations()
	got := Declarations()
	got[0].Inputs[0] = SourceClearing
	got[0].Footprint = "испорчено"
	after := Declarations()
	if after[0].Inputs[0] != before[0].Inputs[0] {
		t.Error("правка копии Inputs протекла в таблицу")
	}
	if after[0].Footprint != before[0].Footprint {
		t.Error("правка копии Footprint протекла в таблицу")
	}
}

// TestWaterDeclarationCarriesWorkedDependency — объявление проекции воды несёт
// зависимость от рабочей поверхности ЯВНО (sqym.12): WaterEdge читает WorkedM
// (terrain.go:684–693), и независимость воды держится ОТКАЗОМ земляному
// эффекту в охранной области реки, а не свойством построения. Следующий автор
// обязан прочитать это в объявлении, а не открыть код. Тест ловит и обратное:
// сняли отказ (surfaceAffecting / riverZoneIntersect) — и правка высот у реки
// потянула бы самый дорогой fan-out графа (см. TestClosureRiverRefusalAllKinds).
func TestWaterDeclarationCarriesWorkedDependency(t *testing.T) {
	d := declaration(Water)
	if d == nil {
		t.Fatal("объявление проекции воды отсутствует")
	}
	text := d.Recipe + "\n" + d.Footprint + "\n" + d.Format
	for _, want := range []string{"рабочей поверхности", "WorkedM", "отказ"} {
		if !strings.Contains(text, want) {
			t.Errorf("объявление воды не несёт зависимость от рабочей поверхности явно: нет %q в объявлении", want)
		}
	}
}
