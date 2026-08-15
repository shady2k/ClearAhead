package track

import (
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// Обход сети: единственный ответ на вопрос «куда ведёт порт».
//
// # Почему это переехало сюда из физики
//
// До 2026-08-15 ответ жил в sim.World.next и был нужен ей одной: физика вела
// точку отсчёта и на границе элемента спрашивала, куда дальше. Потребителей
// стало больше одного, и они лежат по разные стороны импорта:
//
//	физика       ведёт точку отсчёта через порт;
//	отрезок пути наращивается с ведущего конца (Span.GrowA) и обязан спросить
//	             то же самое, теми же правилами.
//
// Отрезок живёт в этом пакете, потому что он выражен в s и в элементах
// компиляции. Оставить обход в физике значило бы либо звать её отсюда (импорт
// идёт в другую сторону), либо написать второй разбор прохода — а разошлись бы
// они на первой же правке правил стрелки, и разошлись бы молча.
//
// # Топология и положение остряков разделены НАРОЧНО
//
// Связность портов — свойство СЕТИ: она не меняется, пока не сменилась ревизия
// карты, и строить её на каждом шаге физики значило бы обходить все элементы
// двести раз в секунду. Положение остряков — свойство ПАРТИИ: оно меняется
// командой, и партия импортирует сеть, а не наоборот.
//
// Поэтому Topology строится один раз и живёт с сетью, а Walk — дешёвое
// значение, которое склеивает её с положением остряков на время одного вопроса.

// Topology — связность портов сети. Строится один раз на компиляцию.
type Topology struct {
	net *CompiledNetwork
	// ends — какие элементы сходятся в порту. Обход всех элементов на каждом
	// шаге физики стоил бы линейного поиска в горячем пути.
	ends map[string][]string
}

// NewTopology строит индекс связности.
func NewTopology(net *CompiledNetwork) *Topology {
	t := &Topology{net: net, ends: map[string][]string{}}
	for id, el := range net.Elements {
		t.ends[el.From] = append(t.ends[el.From], id)
		t.ends[el.To] = append(t.ends[el.To], id)
	}
	return t
}

// Network — сеть, по которой построен индекс. Нужна тем, кто уже держит
// топологию и не хочет тащить рядом второй указатель на то же самое.
func (t *Topology) Network() *CompiledNetwork { return t.net }

// Branch — положение остряка устройства: "straight" либо "diverging".
//
// ФУНКЦИЕЙ, а не картой, и это не про удобство: положение живёт в партии
// (match.Match.Turnouts), партия импортирует сеть, и обратный импорт был бы
// циклом. Функция переносит ответ, не перенося владения.
type Branch func(device string) string

// At склеивает топологию с положением остряков.
//
// Значение, а не указатель: склейка стоит одного присваивания и делается на
// каждом шаге физики. Держать её в поле значило бы держать в сети ссылку на
// партию, то есть завести то самое владение наоборот.
func (t *Topology) At(b Branch) Walk { return Walk{t: t, branch: b} }

// Walk — обход сети при известном положении остряков.
type Walk struct {
	t      *Topology
	branch Branch
}

// Element — элемент по идентификатору.
func (w Walk) Element(id string) (CompiledElement, bool) {
	el, ok := w.t.net.Elements[id]
	return el, ok
}

// Next — куда ведёт порт: элемент и то, каким концом он к порту прилегает
// (0 — началом, 1 — концом).
//
// Отказ означает «дальше ехать нельзя» и покрывает три разных случая одним
// ответом НАРОЧНО: тупик, край карты и стрелка не по ходу — для движения это
// одно и то же событие, тело встаёт. Различать их будет тот, кто станет об этом
// РАССКАЗЫВАТЬ игроку, и различать по своим данным, а не по коду возврата
// (ClearAhead-yqbv).
func (w Walk) Next(from CompiledElement, port string) (CompiledElement, int, bool) {
	for _, id := range w.t.ends[port] {
		if id == from.ID {
			continue
		}
		el := w.t.net.Elements[id]
		// СТРЕЛКА: проход разрешён, только если остряк стоит по нему. Это
		// касается и хода с боку (в реальности — взрез), и он здесь не
		// моделируется: тело просто не едет. Взрез — повреждение устройства, и
		// заводить его надо вместе с последствиями, а не как тихий проезд.
		if dev, branch, isPassage := Passage(id); isPassage {
			if w.branch == nil || w.branch(dev) != branch {
				continue
			}
		}
		if el.From == port {
			return el, 0, true
		}
		return el, 1, true
	}
	return CompiledElement{}, 0, false
}

// Passage — проход ли это устройства, и если да, то какого и какой ветви.
//
// Разбирается ИМЕНЕМ, и это законно ровно потому, что имя прохода собирается в
// одном месте (mapfmt.Turnout.Passages) из идентификатора устройства и суффикса
// ветви. Второй разбор здесь не заводит нового соглашения — он читает то же
// самое соглашение теми же константами.
func Passage(id string) (device, branch string, ok bool) {
	switch {
	case strings.HasSuffix(id, mapfmt.PassageStraight):
		return strings.TrimSuffix(id, mapfmt.PassageStraight), BranchStraight, true
	case strings.HasSuffix(id, mapfmt.PassageDiverging):
		return strings.TrimSuffix(id, mapfmt.PassageDiverging), BranchDiverging, true
	}
	return "", "", false
}

// Ветви прохода. Строки те же, что у mapfmt.Passage.Branch и у положения
// остряка в партии: третьего написания одного и того же не заводится.
const (
	BranchStraight  = "straight"
	BranchDiverging = "diverging"
)
