package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

// model_value.go — ЧИСЛО ТЕЛА, КОТОРОЕ МОЖЕТ ЗАВИСЕТЬ ОТ ЭКЗЕМПЛЯРА.
//
// # Зачем понадобилось
//
// Формат тела (model.go) описывал ЖЁСТКУЮ вещь: переводной механизм у всех
// стрелок одинаков, и его размеры — числа в файле. На доме это кончилось: габарит
// дома приезжает из карты у КАЖДОГО экземпляра свой (width, depth, height), и
// одним файлом на все дома жёсткий формат не обходится.
//
// Обходились до 2026-08-18 тем, что дом рисовал клиент: коробка по габариту плюс
// кровля по четырём своим константам (BUILDING_SINK, ROOF_OVERHANG,
// ROOF_THICKNESS, ROOF_SEAT в world.gd). То есть форму тела выбирал показ — то
// самое, что закон эпика ClearAhead-ax7m запрещает.
//
// # Почему AFFINE, и почему на этом останавливаемся
//
// value = параметр × factor + offset. Ни выражений, ни ссылок на другие части,
// ни условий, ни арифметических деревьев.
//
// Этого ровно хватает дому: стена — width и depth как есть; кровля — width плюс
// два свеса; низ кровли — height минус посадка; толщина — литерал. И ели, когда
// до неё дойдёт: ярусы долями height, радиусы долями width.
//
// ГРАНИЦА ПРОВЕДЕНА ЗДЕСЬ НАРОЧНО. Всё, что сложнее, — шаблонизатор: в файле
// данных заводится язык, у языка появляется порядок вычисления, у порядка —
// расхождение между двумя рендерами. Тело, которое affine не выражает, значит,
// что деталь описана не тем способом, а не что формату не хватает силы.
//
// # Чем это НЕ является: Stretch остаётся
//
// Соблазн заменить Stretch этим механизмом сильный — и он неверен, проверено
// 2026-08-18. Различие не в записи, а в МОМЕНТЕ ВЫЧИСЛЕНИЯ:
//
//   - Value — параметр ЭКЗЕМПЛЯРА, известный при сборке. Дом не меняет ширины,
//     пока стоит. Считается один раз, и каждая часть получает свой размер —
//     ребёнок не едет за родителем.
//   - Stretch — величина, меняющаяся ПРИ ЖИЗНИ УЗЛА: длина переводной тяги
//     меняется на ход остряка, доля хода приходит снапшотом, и клиент двигает
//     тягу каждый кадр перевода (switch_stand.gd::show_position). Применяется
//     масштабом узла, и дети масштабируются вместе с ним — для стержня это
//     верно, для дома было бы неверно: свес кровли поехал бы вместе с шириной.
//
// Два механизма, и оба нужны. Записано затем, что при следующем разборе снова
// покажется, будто их один.

// Value — число тела: либо литерал, либо привязка к параметру экземпляра.
type Value struct {
	// Const — значение, когда By пуст.
	Const float64
	// By — имя параметра экземпляра (Model.Params).
	By string
	// Factor, Offset — коэффициенты привязки: параметр × Factor + Offset.
	Factor float64
	Offset float64
}

// valueBinding — форма привязки на проводе.
//
// Указатели нарочно: JSON не отличает пропущенное число от нуля, а пропущенный
// factor значил бы «умножить на ноль» ровно так же убедительно, как «на
// единицу». Умолчание запрещено правилом проекта, и здесь оно вдобавок
// неотличимо от исправной записи — часть схлопнулась бы в точку молча.
type valueBinding struct {
	By     string   `json:"by"`
	Factor *float64 `json:"factor"`
	Offset *float64 `json:"offset"`
}

// UnmarshalJSON читает число либо привязку.
func (v *Value) UnmarshalJSON(raw []byte) error {
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		*v = Value{Const: n}
		return nil
	}
	var b valueBinding
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return fmt.Errorf("число тела: %s не является ни числом, ни привязкой {by, factor, offset}: %w",
			string(raw), err)
	}
	if b.By == "" {
		return fmt.Errorf("число тела: в привязке %s не назван параметр (by)", string(raw))
	}
	if b.Factor == nil {
		return fmt.Errorf("число тела: привязка к %s без factor — умолчание запрещено, "+
			"пропуск неотличим от «умножить на ноль»", b.By)
	}
	if b.Offset == nil {
		return fmt.Errorf("число тела: привязка к %s без offset — умолчание запрещено", b.By)
	}
	*v = Value{By: b.By, Factor: *b.Factor, Offset: *b.Offset}
	return nil
}

// MarshalJSON пишет обратно тем же видом, каким прочли.
func (v Value) MarshalJSON() ([]byte, error) {
	if v.By == "" {
		return json.Marshal(v.Const)
	}
	return json.Marshal(valueBinding{By: v.By, Factor: &v.Factor, Offset: &v.Offset})
}

// Bound — зависит ли число от параметра экземпляра.
func (v Value) Bound() bool { return v.By != "" }

// Literal — значение, если оно не зависит от экземпляра. Второе значение ложно у
// привязки: спрашивать литерал у привязки — вопрос не по адресу, и отвечать на
// него нулём значило бы дать размер телу, у которого его пока нет.
func (v Value) Literal() (float64, bool) {
	if v.Bound() {
		return 0, false
	}
	return v.Const, true
}

// Resolve считает число для этого экземпляра.
//
// ОТКАЗ, А НЕ ПОДСТАНОВКА, когда параметра нет: тело, объявившее зависимость от
// ширины, без ширины не имеет размера вовсе, и нарисованное «как-нибудь» было бы
// предметом, которого никто не описывал.
func (v Value) Resolve(params map[string]float64) (float64, error) {
	if v.By == "" {
		return v.Const, nil
	}
	p, ok := params[v.By]
	if !ok {
		return 0, fmt.Errorf("параметр %q экземпляру не прислан", v.By)
	}
	out := p*v.Factor + v.Offset
	if math.IsNaN(out) || math.IsInf(out, 0) {
		return 0, fmt.Errorf("параметр %q дал %v", v.By, out)
	}
	return out, nil
}

// finite — годится ли литерал в размер. У привязки проверять нечего до
// подстановки: её значение появляется вместе с экземпляром, и проверяется оно
// там же (Resolve и сборка тела).
func (v Value) finite() bool {
	if v.Bound() {
		return !math.IsNaN(v.Factor) && !math.IsInf(v.Factor, 0) &&
			!math.IsNaN(v.Offset) && !math.IsInf(v.Offset, 0)
	}
	return !math.IsNaN(v.Const) && !math.IsInf(v.Const, 0)
}

// positive — положителен ли ЛИТЕРАЛ. У привязки истинно: см. finite.
func (v Value) positive() bool {
	if v.Bound() {
		return true
	}
	return v.Const > 0 && !math.IsInf(v.Const, 0)
}
