package mapfmt_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// Круговорот запись -> разбор возвращает ту же карту.
//
// # Что именно проверяется, и почему это не тавтология
//
// Проверяется, что запись и разбор — ОДИН формат, а не два похожих. Разбор
// строгий: любое поле, которое запись назовёт иначе, чем ждёт чтение, даёт отказ
// на неизвестное имя. Поэтому тест ловит расхождение тегов, забытое поле в схеме
// и потерю значения при записи одним и тем же падением.
//
// # Почему сравнение точное, хотя проект запрещает сверять float64 байтами
//
// Запрет (4b4d6a7) — про ВЫЧИСЛЕННЫЕ float, которые на другой машине выходят
// иными в последних разрядах. Здесь ничего не вычисляется: json.Marshal пишет
// кратчайшую запись, однозначно восстанавливающую то же самое число, а обратное
// преобразование десятичной записи в double корректно округляется на любой
// машине. Круговорот обязан быть точным, и если он перестанет им быть, знать об
// этом надо.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	cases := map[string]*mapfmt.Map{
		"станция с рельефом": seedmap.Station(seedmap.WithTerrain()),
		"перегон":            seedmap.Line(),
		"заготовка":          seedmap.Blank(),
		"кольцо":             seedmap.Ring(seedmap.RingRadiusM),
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := mapfmt.Encode(&buf, m); err != nil {
				t.Fatalf("запись: %v", err)
			}
			back, err := mapfmt.Decode(&buf)
			if err != nil {
				t.Fatalf("собственная запись не разбирается собственным разбором: %v", err)
			}
			if !reflect.DeepEqual(m, back) {
				t.Fatalf("круговорот изменил карту\nбыло:  %+v\nстало: %+v", m, back)
			}
		})
	}
}
