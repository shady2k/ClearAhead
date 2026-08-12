package mapfmt_test

import (
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// Охват — ДАННЫЕ КАРТЫ с 2026-08-12, и потому предмет валидации, а не доверия.
//
// До этого дня размер мира был константой сервера, и проверять было нечего:
// компилятор не ошибается в числе, которое написано один раз. Как только число
// приехало файлом, ошибиться в нём стало можно молча — забытый блок даёт мир
// радиусом ноль, лишний уровень учетверяет обход, а радиус меньше вылета
// откосов не ломает вообще ничего видимого, кроме подошвы насыпи у края
// коридора.
func TestExtentValidationRefuses(t *testing.T) {
	cases := []struct {
		name  string
		spoil func(*mapfmt.Extent)
		want  string
	}{
		// Забытый блок — главный случай, ради которого счёт идёт штуками:
		// пропущенное поле и явный ноль в JSON неразличимы, и оба обязаны быть
		// отказом.
		{"блока нет вовсе", func(e *mapfmt.Extent) { *e = mapfmt.Extent{} }, "уровней 0"},
		{"уровней ноль", func(e *mapfmt.Extent) { e.Levels = 0 }, "уровней 0"},
		{"уровней отрицательно", func(e *mapfmt.Extent) { e.Levels = -1 }, "уровней -1"},
		{"уровней больше потолка", func(e *mapfmt.Extent) { e.Levels = mapfmt.MaxDetailLevels + 1 }, "вне [1,"},
		{"радиус ноль", func(e *mapfmt.Extent) { e.Level0RadiusM = 0 }, "радиус уровня 0"},
		{"радиус отрицателен", func(e *mapfmt.Extent) { e.Level0RadiusM = -10 }, "радиус уровня 0"},
		// Согласованность двух чисел одной карты: у затравки откосы вылетают на
		// 5 + 21·1,5 = 36,5 м, и радиус в тридцать метров оставляет подошву
		// насыпи на сетке вчетверо грубее.
		{"радиус меньше вылета откосов", func(e *mapfmt.Extent) { e.Level0RadiusM = 30 }, "откосы этой карты вылетают"},
		{"мир больше региона", func(e *mapfmt.Extent) { e.Levels = mapfmt.MaxDetailLevels }, "больше 400000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := seedmap.Station(seedmap.WithTerrain(), seedmap.Mutate(func(m *mapfmt.Map) {
				c.spoil(&m.Terrain.Extent)
			}))
			err := mapfmt.Validate(m)
			if err == nil {
				t.Fatalf("порча %q принята — валидатор молча подставил свой охват", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("отказ не называет причину %q: %v", c.want, err)
			}
		})
	}
}

// Отказ обязан называть ОБА числа, а не одно.
//
// Автор карты видит «радиус мал» и идёт править радиус — а править, может быть,
// надо размах шума: вылет откосов считается из него. Число, которого в тексте
// нет, автор ищет вручную по всей карте.
func TestExtentRefusalNamesBothNumbers(t *testing.T) {
	m := seedmap.Station(seedmap.WithTerrain(), seedmap.Mutate(func(m *mapfmt.Map) {
		m.Terrain.Extent.Level0RadiusM = 10
	}))
	err := mapfmt.Validate(m)
	if err == nil {
		t.Fatal("ожидался отказ")
	}
	for _, want := range []string{"10", "36.5"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в отказе нет числа %s: %v", want, err)
		}
	}
}

// Малый охват законен — иначе «маленькая версия карты» была бы невыразима.
func TestSmallExtentIsAccepted(t *testing.T) {
	m := seedmap.Station(seedmap.WithTerrain(), seedmap.Mutate(func(m *mapfmt.Map) {
		m.Terrain.Extent = mapfmt.Extent{Level0RadiusM: 256, Levels: 3}
	}))
	accepts(t, m)
	if got := m.Terrain.Extent.ReachM(); got != 1024 {
		t.Fatalf("охват %v м, ожидалось 1024 м (256 · 2²)", got)
	}
	if got := m.Terrain.Extent.MaxLevel(); got != 2 {
		t.Fatalf("последний уровень %d, ожидался 2", got)
	}
	// Единственный уровень — тоже законная карта: мир размером с радиус. Ради
	// того, чтобы этот случай остался выразимым, уровни и считаются штуками.
	one := seedmap.Station(seedmap.WithTerrain(), seedmap.Mutate(func(m *mapfmt.Map) {
		m.Terrain.Extent = mapfmt.Extent{Level0RadiusM: 512, Levels: 1}
	}))
	accepts(t, one)
	if got := one.Terrain.Extent.MaxLevel(); got != 0 {
		t.Fatalf("последний уровень %d, ожидался 0", got)
	}
}
