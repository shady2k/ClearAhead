package channel

// coupling_test.go — СЦЕПКА И РАСЦЕПКА ПО ПРОВОДУ.
//
// Домен проверен своими тестами (match/coupling_test.go); здесь проверяется то,
// чего домен не знает: что команда доехала тем же каналом, тем же ключом
// идемпотентности и той же фазой тика, что органы и стрелка, — и что ответ
// несёт СОСТАВ, а не «сцепилось».

import (
	"encoding/json"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/engine"
	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/uuidv7"
)

const (
	loco2ID   = "01a3185c-6002-7242-8242-000001424242"
	lonelyID  = "01a3185c-6005-7242-8242-000004424242"
	newConsID = "01a3185c-6009-7242-8242-000009424242"
)

// touchingMatch — партия из двух машин, стоящих ВПЛОТНУЮ, каждая своим сцепом.
//
// Длина ВЛ80 в наборе фикстуры — 32.84 м, поэтому вторая машина ставится ровно
// на эту длину дальше: концы сходятся, и цеплять есть что.
func touchingMatch(t *testing.T) *match.Match {
	t.Helper()
	net, set := station(t), testSet(t)
	m := &match.Match{ID: "M1", Region: "ST_A", Units: []match.Unit{
		{ID: loco1ID, Name: "LOCO_1", Type: "VL80",
			At: netloc.PointU{Element: seedmap.StationMain, U: 150, Direction: netloc.DirForward}},
		{ID: loco2ID, Name: "LOCO_2", Type: "VL80",
			At: netloc.PointU{Element: seedmap.StationMain, U: 150 + 32.84, Direction: netloc.DirForward}},
		// ТРЕТЬЯ МАШИНА СТОИТ ОДНА, в сорока метрах: у неё соседа нет, и
		// сцепка ей обязана отказать. Без неё проверка «цеплять не с кем»
		// проверялась бы на несуществующей единице, то есть другим отказом.
		{ID: lonelyID, Name: "LOCO_3", Type: "VL80",
			At: netloc.PointU{Element: seedmap.StationMain, U: 60, Direction: netloc.DirForward}},
	}, Controls: map[string]match.Controls{
		loco1ID: match.Stopped(), loco2ID: match.Stopped(), lonelyID: match.Stopped(),
	}}
	st, ok := set.StockType("VL80")
	if !ok {
		t.Fatal("в наборе фикстуры нет паспорта VL80")
	}
	for _, u := range m.Units {
		mo, err := match.StartMotion(u, st, net.Elements[seedmap.StationMain])
		if err != nil {
			t.Fatalf("начальное состояние %s: %v", u.Name, err)
		}
		m.SetMotion(u.ID, mo)
		m.SetConsist(match.Single(u.ID))
	}
	return m
}

func touchingHandler(t *testing.T) *Handler {
	t.Helper()
	return NewHandler(engine.New(touchingMatch(t), nil), uuidv7.Deterministic(), testSet(t), station(t))
}

// consistAnswer — ответ на сцепку в разобранном виде.
type consistAnswer struct {
	Consist match.Consist `json:"consist"`
	Parted  match.Consist `json:"parted"`
}

// СЦЕПКА ПО ПРОВОДУ ОТВЕЧАЕТ СОСТАВОМ ЦЕЛИКОМ.
//
// Проверяется не «пришёл успех», а то, ради чего ответ и устроен так: клиент
// обязан узнать имя нового сцепа И ПОРЯДОК машин в нём. Без порядка он не
// покажет поезд и не назовёт единицу для расцепки — то есть расцепить
// собранный состав будет нечем.
func TestCoupleOverTheWireAnswersWithTheWholeConsist(t *testing.T) {
	h := touchingHandler(t)
	stop := ticking(h.e)
	defer stop()
	st := greeted(t, h)

	r := ask(t, h, st, `{"jsonrpc":"2.0","id":2,"method":"`+MethodCouple+
		`","params":{"unit":"`+loco1ID+`","consist":"`+newConsID+`","command_id":"c1"}}`)
	if r.Error != nil {
		t.Fatalf("сцепка отказала: %+v", r.Error)
	}
	var got consistAnswer
	if err := json.Unmarshal(r.Result, &got); err != nil {
		t.Fatalf("ответ не разбирается: %v (%s)", err, r.Result)
	}
	if got.Consist.ID != newConsID {
		t.Fatalf("новый сцеп назван %q, ожидалось %q", got.Consist.ID, newConsID)
	}
	if len(got.Consist.Members) != 2 {
		t.Fatalf("в ответе %d членов: %+v", len(got.Consist.Members), got.Consist.Members)
	}
	if got.Consist.Members[0].UnitID != loco1ID || got.Consist.Members[1].UnitID != loco2ID {
		t.Fatalf("порядок членов %+v: ожидались LOCO_1, затем LOCO_2", got.Consist.Members)
	}
	if got.Consist.Leading != match.EndA {
		t.Fatalf("ведущий конец %q", got.Consist.Leading)
	}

	// РАСЦЕПКА ВОЗВРАЩАЕТ ОБЕ ЧАСТИ. Отцепленная — та, что за названной
	// единицей; своё имя она получила из команды.
	back := ask(t, h, st, `{"jsonrpc":"2.0","id":3,"method":"`+MethodUncouple+
		`","params":{"unit":"`+loco1ID+`","consist":"`+loco2ID+`","command_id":"c2"}}`)
	if back.Error != nil {
		t.Fatalf("расцепка отказала: %+v", back.Error)
	}
	var split consistAnswer
	if err := json.Unmarshal(back.Result, &split); err != nil {
		t.Fatalf("ответ расцепки не разбирается: %v (%s)", err, back.Result)
	}
	if len(split.Consist.Members) != 1 || split.Consist.Members[0].UnitID != loco1ID {
		t.Fatalf("оставшаяся часть: %+v", split.Consist)
	}
	if len(split.Parted.Members) != 1 || split.Parted.Members[0].UnitID != loco2ID {
		t.Fatalf("отцепленная часть: %+v", split.Parted)
	}
}

// ОТКАЗЫ ПРИХОДЯТ ПО ПРОВОДУ ОТКАЗАМИ, а не пустым успехом.
//
// КАЖДЫЙ СЛУЧАЙ НА СВОЁМ ОБРАБОТЧИКЕ, и это не чистоплюйство: команды меняют
// мир, и первая же прошедшая сцепка сделала бы следующий случай проверкой
// другого мира. Первый заход так и вышел — «расцепка за последней единицей»
// прошла, потому что предыдущий случай успел собрать состав.
func TestCouplingRefusalsOverTheWire(t *testing.T) {
	for _, c := range []struct {
		name, frame string
	}{
		{"единица не названа", `{"jsonrpc":"2.0","id":2,"method":"` + MethodCouple +
			`","params":{"consist":"` + newConsID + `"}}`},
		{"имя нового сцепа пусто", `{"jsonrpc":"2.0","id":3,"method":"` + MethodCouple +
			`","params":{"unit":"` + loco1ID + `","consist":""}}`},
		{"имя нового сцепа с разделителем адреса", `{"jsonrpc":"2.0","id":4,"method":"` + MethodCouple +
			`","params":{"unit":"` + loco1ID + `","consist":"TRA/IN"}}`},
		{"неизвестное поле", `{"jsonrpc":"2.0","id":5,"method":"` + MethodCouple +
			`","params":{"unit":"` + loco1ID + `","consist":"` + newConsID + `","куда":"туда"}}`},
		{"расцепка одиночной машины", `{"jsonrpc":"2.0","id":6,"method":"` + MethodUncouple +
			`","params":{"unit":"` + loco1ID + `","consist":"` + newConsID + `"}}`},
		{"сцепка машины, у которой нет соседа", `{"jsonrpc":"2.0","id":7,"method":"` + MethodCouple +
			`","params":{"unit":"` + lonelyID + `","consist":"` + newConsID + `"}}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := touchingHandler(t)
			stop := ticking(h.e)
			defer stop()
			st := greeted(t, h)
			if r := ask(t, h, st, c.frame); r.Error == nil {
				t.Fatalf("команда прошла: %s", r.Result)
			}
		})
	}
}
