package mapfmt

import (
	"strings"
	"testing"
)

// Зеркало таблицы tID* из helpers_test.go (пакет mapfmt_test) для тестов
// ВНУТРЕННЕГО пакета: внешний пакет не виден, а значения обязаны совпадать —
// два одинаковых UUID в одной карте дали бы отказ не по предмету теста.
// Номера строк в комментариях повторяют строки внешней таблицы.
const (
	uIDN1  = "01a3185c-5001-7242-8242-000000424242" // метка N1 (tID00)
	uIDSW  = "01a3185c-5007-7242-8242-000006424242" // метка SW (tID06)
	uIDEA  = "01a3185c-5010-7242-8242-00000f424242" // метка EA (tID15)
	uIDEB  = "01a3185c-5011-7242-8242-000010424242" // метка EB (tID16)
	uIDEC  = "01a3185c-5012-7242-8242-000011424242" // метка EC (tID17)
	uIDED  = "01a3185c-5013-7242-8242-000012424242" // метка ED (tID18)
	uIDEE  = "01a3185c-5014-7242-8242-000013424242" // метка EE (tID19)
	uIDEF  = "01a3185c-5015-7242-8242-000014424242" // метка EF (tID20)
	uIDSW1 = "01a3185c-5016-7242-8242-000015424242" // метка SW1 (tID21)
	uIDSW2 = "01a3185c-5017-7242-8242-000016424242" // метка SW2 (tID22)

	// uIDE1 — зеркало seedmap.LineEdgeID (метка E1): внутренний пакет не
	// может импортировать seedmap (цикл импорта), а значение обязано
	// совпадать с фабрикой.
	uIDE1 = "018bcfe5-6803-7242-8242-000003424242"
)

// Разделители составных адресов не бывают внутри имени. Каждый случай ниже —
// не гипотеза, а закрытая дыра.
func TestSeparatorsAreForbiddenInIdentifier(t *testing.T) {
	cases := []struct {
		id     string
		hazard string
	}{
		{"SW:straight", "ребро с таким именем молча подменяло геометрию прохода стрелки"},
		{"N1.P1", "порт становится неоднозначным: узел A + порт B.C и узел A.B + порт C дают один адрес"},
		{"E|1", "поток хеша разделён вертикальной чертой — две разные карты дали бы один network_model_hash"},
		{"E/1", "map_id уезжает сегментом URL: карта стала бы недостижимой молча, а не отказом"},
		{"E@2", "редактор использует собачку при разрезании ребра"},
	}
	for _, c := range cases {
		if err := ValidID("ребро", c.id); err == nil {
			t.Errorf("принят идентификатор %q, хотя %s", c.id, c.hazard)
		}
	}
}

func TestControlCharsAndSpacesInIdentifier(t *testing.T) {
	bad := []string{
		"E\n1",     // ломает построчный поток хеша
		"E‮1",      // RTL-override: два разных ID выглядят одинаково
		" E1",      // ведущий пробел
		"E1 ",      // хвостовой
		"",         // пустой
		"\xff\xfe", // некорректный UTF-8
		strings.Repeat("E", MaxIDLength+1),
	}
	for _, id := range bad {
		if err := ValidID("ребро", id); err == nil {
			t.Errorf("принят идентификатор %q", id)
		}
	}
}

func TestLegalIdentifiersAreAccepted(t *testing.T) {
	good := []string{"ST_A", "ST_A_SW_1", "TRACK_MAIN_1520", "RUN_ST_A_E_T1", "Путь_2", "E1_CONT"}
	for _, id := range good {
		if err := ValidID("ребро", id); err != nil {
			t.Errorf("отвергнут законный идентификатор %q: %v", id, err)
		}
	}
}

// Ребро, названное как проход стрелки, отвергается картой целиком.
//
// Раньше такая карта проходила сбор элементов (дубликат искался только среди
// рёбер), а в общей таблице выравниваний проход МОЛЧА затирал геометрию
// ребра: объявленное автором просто исчезало. Защита была двухслойной — запрет
// разделителя в авторском идентификаторе (ValidID) и сбор элементов.
//
// С решением «UUIDv7 везде» тождество элемента — UUID, и колонка в нём
// невозможна: имя прохода (uuid:straight) отвергается уже проверкой формы
// UUIDv7, раньше запрета разделителя. Тест закрепляет, что карта с ребром под
// именем прохода всё ещё не проходит вход: причина изменилась, а дефект
// (молчаливое затирание) остался недостижим.
func TestEdgeNamedAsPassageIsRejected(t *testing.T) {
	m := &Map{
		FormatVersion: FormatVersion,
		MapID:         "ST_A",
		MapRevision:   1,
		Anchors:       map[string]Anchor{uIDN1 + ".P1": {}},
		Topology: Topology{
			Nodes: []Node{{ID: uIDN1, Name: "N1", Ports: []Port{{ID: "P1", Purpose: "map_boundary"}}}},
			Turnouts: []Turnout{
				{ID: uIDSW, Name: "SW", Kind: KindRail, Hand: "right", Ports: TurnoutPorts{Common: "C", Straight: "S", Diverging: "D"}},
			},
			Edges: []Edge{{ID: uIDSW + PassageStraight, Name: "E_FAKE", Kind: KindRail, From: uIDN1 + ".P1", To: uIDN1 + ".P1"}},
		},
		Geometry: Geometry{
			Turnouts: map[string]TurnoutGeometry{uIDSW: {
				Straight:  Alignments{Horizontal: []HPrim{{Kind: "straight", Length: 10}}},
				Diverging: Alignments{Horizontal: []HPrim{{Kind: "straight", Length: 10}}},
			}},
			Edges: map[string]Alignments{
				uIDSW + PassageStraight: {Horizontal: []HPrim{{Kind: "straight", Length: 999}}},
			},
		},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("карта с ребром под именем прохода принята")
	}
	if !strings.Contains(err.Error(), "не UUIDv7") {
		t.Fatalf("отказ пришёл не по той причине: %v", err)
	}
}

// Повтор Turnout.ID проверяется в duplicate_test.go: там же разобрано, чем
// такая карта отвергалась ДО появления проверки повтора. Здесь ему не место —
// это инвариант топологии, а не правило записи идентификатора.
