package track

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// Фикстура геометрии — та же, что у крестовины (frogEls): прямой проход прямая
// 33.5 м, боковой — дуга R=300 на −0.1107 рад, то есть ровно ST_A_SW_1. Своя
// геометрия здесь означала бы, что брусья проверяются не на той стрелке, на
// которой считается крестовина.

// timberTypes — тип с брусьями. Числа затравки: шпала 2.75 м, брус шагом 0.5 м,
// комплект до 5.5 м.
func timberTypes(lengthMax float64) map[string]mapfmt.TrackType {
	return map[string]mapfmt.TrackType{
		seedmap.TrackTypeID: {
			ID:      seedmap.TrackTypeID,
			Name:    "TRACK_MAIN_1520",
			Gauge:   1.520,
			Sleeper: mapfmt.TrackSleeper{Pitch: 0.543, Length: 2.75, Width: 0.28, Height: 0.20},
			Timber:  &mapfmt.TrackTimber{Pitch: 0.50, LengthMax: lengthMax, Width: 0.30, Height: 0.20},
		},
	}
}

func sw1Grid(t *testing.T, lengthMax float64) *RenderTurnoutGrid {
	t.Helper()
	els := frogEls(mustChain(t, primStraight(t, 33.5)), mustChain(t, primArc(t, 300, -0.1107)))
	// Решётка БЕЗ ПРИВОДА: этот помощник проверяет правило длины как таковое, а
	// удлинение под станину — своим тестом. Передай сюда привод, и оба правила
	// мерились бы одним числом.
	g, err := turnoutGrid(els, timberTypes(lengthMax), frogConstruction(), sw1Right(),
		RenderTurnoutDrive{}, false)
	if err != nil {
		t.Fatalf("решётка стрелки: %v", err)
	}
	return g
}

// TestTimberGridCoversDeviceHalfOpen — правило диапазона у брусьев то же, что у
// прогона (§4): phase + n·pitch ∈ [0, длина). Проверяется следствие, которое
// видно: устройство накрыто целиком, но брус в конечной точке не ставится —
// иначе он сдвоился бы с первой шпалой примыкающего прогона.
func TestTimberGridCoversDeviceHalfOpen(t *testing.T) {
	g := sw1Grid(t, 5.5)
	if len(g.Timbers) == 0 {
		t.Fatal("решётка пуста")
	}
	// 33.5 / 0.5 = 67 ровно, и полуоткрытое правило даёт 67 брусьев: 0.0 … 33.0.
	if len(g.Timbers) != 67 {
		t.Fatalf("брусьев %d, ожидалось 67 при длине 33.5 м и шаге 0.5 м", len(g.Timbers))
	}
	if g.Timbers[0].U != 0 {
		t.Fatalf("первый брус на u=%g, ожидался 0", g.Timbers[0].U)
	}
	last := g.Timbers[len(g.Timbers)-1].U
	if last >= 33.5 {
		t.Fatalf("последний брус на u=%g — конечная точка занята, будет сдвоение с прогоном", last)
	}
	if 33.5-last > 0.5+1e-9 {
		t.Fatalf("от последнего бруса до конца устройства %g м — больше шага", 33.5-last)
	}
}

// TestTimberLengthGrowsWithSpread — длина бруса есть длина шпалы плюс
// расхождение осей. Проверяются оба конца: у носка пути совпадают, и брус равен
// шпале; к хвосту он длиннее ровно на расхождение.
func TestTimberLengthGrowsWithSpread(t *testing.T) {
	g := sw1Grid(t, 5.5)
	first := g.Timbers[0]
	if math.Abs(first.Length-2.75) > 1e-9 {
		t.Fatalf("брус у носка %g м, ожидалась длина шпалы 2.75", first.Length)
	}
	if math.Abs(first.Offset) > 1e-9 {
		t.Fatalf("брус у носка смещён на %g м — у носка пути совпадают", first.Offset)
	}
	prev := 0.0
	for i, tb := range g.Timbers {
		if tb.Length < prev-1e-9 {
			t.Fatalf("брус %d короче предыдущего: %g против %g", i, tb.Length, prev)
		}
		prev = tb.Length
	}
	// Хвост: дуга R=300 на 33 м уводит боковой путь на R(1−cos(u/R)).
	last := g.Timbers[len(g.Timbers)-1]
	wantSpread := 300 * (1 - math.Cos(last.U/300))
	if d := math.Abs(last.Length - (2.75 + wantSpread)); d > 1e-6 {
		t.Fatalf("хвостовой брус %g м, ожидалось 2.75 + %g = %g",
			last.Length, wantSpread, 2.75+wantSpread)
	}
	// Центр — посередине между дальними краями, то есть на половине расхождения.
	// Знак отрицательный: правая стрелка уводит боковой путь ВПРАВО, а смещение
	// меряется по ЛЕВОЙ нормали.
	if d := math.Abs(last.Offset - (-wantSpread / 2)); d > 1e-6 {
		t.Fatalf("центр хвостового бруса на %g м, ожидалось %g", last.Offset, -wantSpread/2)
	}
}

// TestTimberLengthStopsAtSetLimit — комплект кончается. Проверка стоит ЗДЕСЬ, а
// не на клиенте, и это решение: числа комплекта в проводе нет (клиенту для
// рисования оно не нужно), поэтому клиентская проверка сверялась бы с
// бесконечностью и покраснеть не могла бы.
//
// Предел взят заведомо низким — 3.0 м против 4.563 м, до которых доходит
// настоящий комплект на этой стрелке: с 5.5 м упор не наступает вовсе, и
// проверка проверяла бы отсутствие события.
func TestTimberLengthStopsAtSetLimit(t *testing.T) {
	g := sw1Grid(t, 3.0)
	capped := 0
	for i, tb := range g.Timbers {
		if tb.Length > 3.0+1e-9 {
			t.Fatalf("брус %d длиной %g м при комплекте 3.0", i, tb.Length)
		}
		if math.Abs(tb.Length-3.0) < 1e-9 {
			capped++
		}
	}
	if capped == 0 {
		t.Fatal("ни один брус не упёрся в комплект — проверка ничего не проверила")
	}
	// БЛИЖНИЙ КРАЙ НЕПОДВИЖЕН, и это то, ради чего упор считается несимметрично.
	// Край со стороны прямого пути обязан остаться на −sleeper.Length/2 = −1.375
	// м: растяни брус симметрично, и под прямым путём кончился бы вылет.
	last := g.Timbers[len(g.Timbers)-1]
	near := last.Offset + last.Length/2 // сход вправо: ближний край — левый, то есть плюс
	if d := math.Abs(near - 2.75/2); d > 1e-9 {
		t.Fatalf("ближний край упёршегося бруса на %g м, ожидалось %g", near, 2.75/2)
	}
}

// TestTimberGridRefusesTypeWithoutTimber — валидатор карты такую карту не
// выпустит, но компилятор не полагается на то, что его позвали после него:
// стрелка без решётки — это ровно дефект ClearAhead-7kv, и молчаливо
// возвращать её нельзя.
func TestTimberGridRefusesTypeWithoutTimber(t *testing.T) {
	els := frogEls(mustChain(t, primStraight(t, 33.5)), mustChain(t, primArc(t, 300, -0.1107)))
	_, err := turnoutGrid(els, frogTypes(), frogConstruction(), sw1Right(), RenderTurnoutDrive{}, false)
	if err == nil {
		t.Fatal("тип без блока timber принят — стрелка осталась бы без решётки молча")
	}
}

// TestTimberGridOnShippedMap — сквозная проверка на боевой карте: обе стрелки
// ST_A получают решётку, и она лежит на осевой ПРЯМОГО прохода.
func TestTimberGridOnShippedMap(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция затравки: %v", err)
	}
	if len(rg.TurnoutGrids) != len(m.Topology.Turnouts) {
		t.Fatalf("решёток %d при %d стрелках", len(rg.TurnoutGrids), len(m.Topology.Turnouts))
	}
	for _, g := range rg.TurnoutGrids {
		if g.Element != g.Owner+mapfmt.PassageStraight {
			t.Fatalf("решётка %s опирается на %s, а не на прямой проход", g.Owner, g.Element)
		}
		if len(g.Timbers) == 0 {
			t.Fatalf("решётка %s пуста", g.Owner)
		}
		if g.Width <= 0 || g.Height <= 0 {
			t.Fatalf("решётка %s без сечения бруса: %g × %g", g.Owner, g.Width, g.Height)
		}
	}
}

// geom импортируется ради фикстур примитивов, общих с крестовиной.
var _ = geom.Chain{}

// TestTimbersCarryTheDrive — ПРИВОД СТОИТ НА БРУСЬЯХ, А НЕ ЗА ИХ КОНЦОМ.
//
// Замер, ради которого правило заведено (ST_A, 2026-08-16): брус в сечении
// привода был 2.757 м — полудлина 1.378, — станина отнесена на 1.875, полуширина
// балласта 1.75. Ящик стоял на откосе призмы, ни на что не опираясь; владелец
// нашёл это глазами на кадре.
//
// Проверяется СЛЕДСТВИЕ, которое видно: край бруса со стороны станины дальше
// самой станины, и только у брусьев в окне вокруг неё — иначе под приводом
// вырос бы помост во всю длину перевода.
func TestTimbersCarryTheDrive(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	drives := map[string]RenderTurnoutDrive{}
	for _, d := range rg.TurnoutDrives {
		drives[d.Owner] = d
	}
	if len(drives) == 0 {
		t.Fatal("приводов не посчитано — проверять нечего")
	}
	for _, g := range rg.TurnoutGrids {
		d, ok := drives[g.Owner]
		if !ok {
			continue
		}
		// Сторона станины: знак выноса. Край бруса с этой стороны — центр плюс
		// половина длины в ту же сторону.
		side := math.Copysign(1, d.Offset)
		under, wide := 0, 0
		for _, tb := range g.Timbers {
			edge := (tb.Offset + side*tb.Length/2) * side // насколько ушёл от оси в сторону станины
			near := math.Abs(tb.U-d.U) <= DriveTimberWindow
			if near {
				under++
				if edge < math.Abs(d.Offset) {
					t.Fatalf("стрелка %s: брус на u=%.2f уходит к станине на %.3f м, а станина на %.3f м — она за концом бруса",
						g.Owner, tb.U, edge, math.Abs(d.Offset))
				}
			}
			if edge > math.Abs(d.Offset) {
				wide++
			}
		}
		if under < 2 {
			t.Fatalf("стрелка %s: под приводом %d бруса — станина повиснет между ними", g.Owner, under)
		}
		// ОКНО, А НЕ ПОМОСТ: длинные брусья только под приводом. Число берётся из
		// шага решётки — сколько брусьев попадает в окно, столько и вправе быть
		// длинными.
		if wide > under {
			t.Fatalf("стрелка %s: длинных брусьев %d при %d в окне привода — помост вместо опоры",
				g.Owner, wide, under)
		}
	}
}
