package mapfmt_test

import (
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// Валидатор ОТКАЗЫВАЕТ, а не чинит: река с невозможным размером обязана быть
// отвергнута на входе, а не подставлена правдоподобным значением.
func TestRiverValidationRefuses(t *testing.T) {
	cases := []struct {
		name  string
		spoil func(*mapfmt.River)
		want  string
	}{
		{"одна точка оси", func(r *mapfmt.River) { r.Axis = r.Axis[:1] }, "точек оси"},
		{"нулевая полуширина", func(r *mapfmt.River) { r.HalfWidthM = 0 }, "half_width"},
		{"отрицательный берег", func(r *mapfmt.River) { r.BankM = -1 }, "bank"},
		{"нулевая глубина", func(r *mapfmt.River) { r.DepthM = 0 }, "depth"},
		// Бровка вровень с урезом — река, налитая по края: любая неровность
		// шума выпустит воду, и увидеть это можно только с воздуха.
		{"бровка вровень с урезом", func(r *mapfmt.River) { r.RimM = 0 }, "rim"},
		{"отрицательная долина", func(r *mapfmt.River) { r.ValleyM = -1 }, "valley"},
		{"отрицательный пляж", func(r *mapfmt.River) { r.SandBandM = -1 }, "sand_band"},
		{"пустой идентификатор", func(r *mapfmt.River) { r.ID = "" }, "река"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := seedmap.Station(seedmap.WithTerrain(), seedmap.Mutate(func(m *mapfmt.Map) {
				c.spoil(&m.Objects.Rivers[0])
			}))
			err := mapfmt.Validate(m)
			if err == nil {
				t.Fatalf("порча %q принята — валидатор молча подставил своё", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("отказ не называет причину %q: %v", c.want, err)
			}
		})
	}
}

// Две реки с одним идентификатором — отказ: ID адресует объект, и второй с тем
// же именем делает адресацию неоднозначной.
func TestDuplicateRiverIDRefused(t *testing.T) {
	m := seedmap.Station(seedmap.WithTerrain(), seedmap.Mutate(func(m *mapfmt.Map) {
		m.Objects.Rivers = append(m.Objects.Rivers, m.Objects.Rivers[0])
	}))
	err := mapfmt.Validate(m)
	if err == nil || !strings.Contains(err.Error(), "дважды") {
		t.Fatalf("дубликат реки принят: %v", err)
	}
}

// Карта без блока рек законна: реки может не быть.
func TestMapWithoutRiversIsValid(t *testing.T) {
	m := seedmap.Station(seedmap.WithTerrain(), seedmap.Mutate(func(m *mapfmt.Map) {
		m.Objects.Rivers = nil
	}))
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("карта без реки отвергнута: %v", err)
	}
}
