package mapfmt

import (
	"strings"
	"testing"
)

// Разделители составных адресов не бывают внутри имени. Каждый случай ниже —
// не гипотеза, а закрытая дыра.
func TestРазделителиЗапрещеныВИдентификаторе(t *testing.T) {
	cases := []struct {
		id     string
		опасен string
	}{
		{"SW:straight", "ребро с таким именем молча подменяло геометрию прохода стрелки"},
		{"N1.P1", "порт становится неоднозначным: узел A + порт B.C и узел A.B + порт C дают один адрес"},
		{"E|1", "поток хеша разделён вертикальной чертой — две разные карты дали бы один track_hash"},
		{"E/1", "map_id уезжает сегментом URL: карта стала бы недостижимой молча, а не отказом"},
		{"E@2", "редактор использует собачку при разрезании ребра"},
	}
	for _, c := range cases {
		if err := ValidID("ребро", c.id); err == nil {
			t.Errorf("принят идентификатор %q, хотя %s", c.id, c.опасен)
		}
	}
}

func TestУправляющиеИПробелыВИдентификаторе(t *testing.T) {
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

func TestЗаконныеИдентификаторыПринимаются(t *testing.T) {
	good := []string{"ST_A", "ST_A_SW_1", "TRACK_MAIN_1435", "RUN_ST_A_E_T1", "Путь_2", "E1_CONT"}
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
// ребра: объявленное автором просто исчезало. Проверка держится на запрете
// разделителя, поэтому тест стоит здесь, а не в валидаторе топологии.
func TestРеброПодИменемПроходаОтвергается(t *testing.T) {
	m := &Map{
		FormatVersion: FormatVersion,
		MapID:         "ST_A",
		MapRevision:   1,
		Anchors:       map[string]Anchor{"N1.P1": {}},
		Topology: Topology{
			Nodes: []Node{{ID: "N1", Ports: []Port{{ID: "P1", Purpose: "map_boundary"}}}},
			Turnouts: []Turnout{
				{ID: "SW", Hand: "right", Ports: TurnoutPorts{Common: "C", Straight: "S", Diverging: "D"}},
			},
			Edges: []Edge{{ID: "SW" + PassageStraight, From: "N1.P1", To: "N1.P1"}},
		},
		Geometry: Geometry{
			Turnouts: map[string]TurnoutGeometry{"SW": {
				Straight:  Alignments{Horizontal: []HPrim{{Kind: "straight", Length: 10}}},
				Diverging: Alignments{Horizontal: []HPrim{{Kind: "straight", Length: 10}}},
			}},
			Edges: map[string]Alignments{
				"SW" + PassageStraight: {Horizontal: []HPrim{{Kind: "straight", Length: 999}}},
			},
		},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("карта с ребром под именем прохода принята")
	}
	if !strings.Contains(err.Error(), "запрещён в идентификаторе") {
		t.Fatalf("отказ пришёл не по той причине: %v", err)
	}
}

// Повтор Turnout.ID проверяется в duplicate_test.go: там же разобрано, чем
// такая карта отвергалась ДО появления проверки повтора. Здесь ему не место —
// это инвариант топологии, а не правило записи идентификатора.
