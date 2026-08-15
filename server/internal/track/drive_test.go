package track

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// Фикстура та же, что у крестовины и брусьев (frogEls): прямой проход прямая
// 33.5 м, боковой — дуга R=300 на −0.1107 рад, то есть ST_A_SW_1. Своя
// геометрия означала бы, что привод проверяется не на той стрелке, на которой
// считается всё остальное устройство.

func sw1Drive(t *testing.T, turn mapfmt.Turnout) *RenderTurnoutDrive {
	t.Helper()
	els := frogEls(mustChain(t, primStraight(t, 33.5)), mustChain(t, primArc(t, 300, -0.1107)))
	d, err := turnoutDrive(els, timberTypes(5.5), frogConstruction(), turn)
	if err != nil {
		t.Fatalf("привод стрелки: %v", err)
	}
	return d
}

// TestDriveStandsAwayFromDivergingSide — привод стоит С ТОЙ СТОРОНЫ, В КОТОРУЮ
// БОКОВОЙ ПУТЬ НЕ УХОДИТ.
//
// Это главное свойство постановки, и проверяется оно знаком: у правой стрелки
// боковой путь уходит вправо (по левой нормали — в минус), значит вынос обязан
// быть положительным. Ошибись знак — станина встала бы посреди бокового пути, и
// на кадре это выглядело бы как предмет между рельсами.
func TestDriveStandsAwayFromDivergingSide(t *testing.T) {
	d := sw1Drive(t, sw1Right())
	if d.Offset <= 0 {
		t.Fatalf("вынос %g: у правой стрелки привод обязан стоять слева от прямого пути", d.Offset)
	}
	// Шпала 2.75 м, отступ 0.5 м: 1.375 + 0.5.
	want := 2.75/2 + DriveClearance
	if math.Abs(d.Offset-want) > 1e-9 {
		t.Fatalf("вынос %g м, ожидался %g м (полшпалы плюс отступ)", d.Offset, want)
	}
	if d.U != DriveAtU {
		t.Fatalf("привод на u=%g м, ожидался %g м", d.U, DriveAtU)
	}
	if d.Element != seedmap.StationSW1+mapfmt.PassageStraight {
		t.Fatalf("привод адресован элементу %s, а опорная линия устройства — прямой проход", d.Element)
	}
}

// TestDriveSideFollowsGeometryNotHand — сторона выводится ИЗ ГЕОМЕТРИИ, а не из
// авторской пометки о рукости.
//
// Карта, где рукость сказана одна, а боковой путь уходит в другую, невозможна у
// валидатора и потому собирается здесь руками: предмет проверки — источник
// решения, а не перечень значений. Разойдись они однажды — привод обязан
// согласоваться с рельсами, а не с пометкой, иначе он встанет в габарит
// бокового пути, которого автор в пометке не назвал.
func TestDriveSideFollowsGeometryNotHand(t *testing.T) {
	els := frogEls(mustChain(t, primStraight(t, 33.5)), mustChain(t, primArc(t, 300, +0.1107)))
	lying := sw1Right() // пометка говорит "right", дуга уходит влево
	d, err := turnoutDrive(els, timberTypes(5.5), frogConstruction(), lying)
	if err != nil {
		t.Fatalf("привод стрелки: %v", err)
	}
	if d.Offset >= 0 {
		t.Fatalf("вынос %g: боковой путь уходит влево, привод обязан уйти вправо", d.Offset)
	}
}

// TestDriveCarriesMechanismAndPlate — привод несёт вид механизма и метку
// стрелки: по первому клиент выбирает тело, по второй — то, что написано на
// табличке.
func TestDriveCarriesMechanismAndPlate(t *testing.T) {
	turn := sw1Right()
	turn.Name = "SW1"
	turn.Drive = mapfmt.DriveElectric
	d := sw1Drive(t, turn)
	if d.Drive != mapfmt.DriveElectric {
		t.Fatalf("механизм %q, а в карте %q", d.Drive, mapfmt.DriveElectric)
	}
	if d.Name != "SW1" {
		t.Fatalf("метка %q, а на табличке обязана быть метка стрелки %q", d.Name, "SW1")
	}
	if d.Owner != seedmap.StationSW1 {
		t.Fatalf("владелец %q, ожидался %q", d.Owner, seedmap.StationSW1)
	}
}

// TestStationHasDriveForEveryTurnout — у КАЖДОЙ стрелки станции есть привод, и
// порядок их вывода канонический.
//
// Проверка на затравке, а не на фикстуре: пропущенное устройство — это стрелка,
// которую нечем перевести и у которой нет таблички, и заметно это было бы
// только глазами на кадре.
func TestStationHasDriveForEveryTurnout(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if len(rg.TurnoutDrives) != len(m.Topology.Turnouts) {
		t.Fatalf("приводов %d, стрелок %d", len(rg.TurnoutDrives), len(m.Topology.Turnouts))
	}
	for i := 1; i < len(rg.TurnoutDrives); i++ {
		if rg.TurnoutDrives[i-1].Owner >= rg.TurnoutDrives[i].Owner {
			t.Fatalf("порядок приводов не канонический: %s перед %s",
				rg.TurnoutDrives[i-1].Owner, rg.TurnoutDrives[i].Owner)
		}
	}
	kinds := map[string]string{}
	for _, d := range rg.TurnoutDrives {
		kinds[d.Owner] = d.Drive
	}
	// Затравка повторяет ST_A: первая стрелка ручная, вторая с электроприводом.
	// Пара нужна не для красоты — на ней проверяется, что механизм доезжает
	// разным, а не одинаковым для всех.
	if kinds[seedmap.StationSW1] != mapfmt.DriveManual {
		t.Fatalf("SW1 приехала с механизмом %q, ожидался %q", kinds[seedmap.StationSW1], mapfmt.DriveManual)
	}
	if kinds[seedmap.StationSW2] != mapfmt.DriveElectric {
		t.Fatalf("SW2 приехала с механизмом %q, ожидался %q", kinds[seedmap.StationSW2], mapfmt.DriveElectric)
	}
}
