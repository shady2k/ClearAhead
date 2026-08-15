package match

import (
	"fmt"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// ЗАНЯТОСТЬ ПУТИ: обратный индекс и запрет наложения.
//
// # Зачем индекс, если тела можно просто перебрать
//
// Потому что вопрос задаётся не так. «Чей это отрезок» спрашивают ПРО МЕСТО:
// можно ли перевести остряк под этим устройством, свободна ли секция впереди,
// не столкнутся ли двое на разных ветвях одной стрелки. Перебор всех тел мира
// ради ответа про один элемент — это линейный поиск в том месте, где ответ
// нужен на каждом шаге физики каждого тела, то есть квадрат по числу тел.
//
// Индекс отвечает за время, пропорциональное числу тел НА ЭТОМ элементе, а их
// там единицы.
//
// # «В той же транзакции» — это про единственного писателя
//
// Индекс правится ровно там, где правится отрезок, — в SetMotion, и больше
// нигде. Второй писатель означал бы окно, в котором индекс говорит про мир,
// которого уже нет; а поймать такое окно тестом почти невозможно, потому что
// оно закрывается само следующим шагом.
//
// Сторожем этому служит differential-тест: полный пересчёт индекса обязан дать
// то же, что накопленный по одному телу за прогон (TestOccupancyRebuildMatches).
//
// # Чего здесь НЕТ
//
// Секций обнаружения, замыканий и маршрутов. Занятость здесь — геометрическая:
// какое тело какой кусок какого элемента накрывает. Эксплуатационная занятость
// (секция занята, пока рельсовая цепь замкнута) — другая величина, у неё другой
// потребитель (централизация, ClearAhead-duf), и объявлять её сейчас значило бы
// объявить форму без потребителя.

// OccupationRef — ССЫЛКА на визит, занимающий элемент.
//
// Ссылка, а не копия интервала, и это то же правило, что у пары портов у визита
// (track.VisitPorts): копия координат была бы вторым написанием того, что уже
// записано в отрезке, и разошлась бы с ним ровно в тот миг, когда тело
// сдвинулось, а индекс не обновили. Ссылка разойтись не может — у неё нечему
// расходиться.
type OccupationRef struct {
	// Unit — чей отрезок.
	Unit string
	// Visit — номер визита в отрезке этого тела.
	Visit int
}

// Interval — сам занятый интервал по ссылке. Второе значение ложно, если
// ссылка протухла: тела нет в партии либо визита нет в его отрезке.
func (r OccupationRef) Interval(m Match) (netloc.IntervalS, bool) {
	mo, ok := m.Motions[r.Unit]
	if !ok || r.Visit < 0 || r.Visit >= len(mo.Span) {
		return netloc.IntervalS{}, false
	}
	return mo.Span[r.Visit], true
}

// Occupancy — обратный индекс занятости: по элементу — ссылки на визиты.
//
// Порядок внутри элемента КАНОНИЧЕСКИЙ (тело, затем номер визита), и это не
// украшение: без него полный пересчёт и накопленный по одному телу давали бы
// разные списки при одинаковом содержимом, и differential-тест сравнивал бы
// порядок обхода карты Go вместо самого индекса.
type Occupancy map[string][]OccupationRef

// occupancy — индекс, заведённый при первом обращении.
func (m *Match) occupancy() Occupancy {
	if m.Occupied == nil {
		m.Occupied = Occupancy{}
	}
	return m.Occupied
}

// setOccupancy — переложить занятость одного тела. Зовётся из SetMotion и
// больше ниоткуда: разбор — в шапке файла.
func (m *Match) setOccupancy(unitID string, before, after track.Span) {
	idx := m.occupancy()
	// Сначала снимаем старое — ВСЁ старое, а не только то, что перестало
	// пересекаться. Разница видна на теле, ушедшем с элемента целиком: его
	// ссылка осталась бы висеть, и элемент выглядел бы занятым навсегда.
	for _, el := range before.Elements() {
		kept := idx[el][:0]
		for _, ref := range idx[el] {
			if ref.Unit != unitID {
				kept = append(kept, ref)
			}
		}
		if len(kept) == 0 {
			// Пустой список и отсутствие ключа обязаны быть одним и тем же: иначе
			// индекс копил бы имена элементов, по которым когда-то проезжали, и
			// полный пересчёт с ним не сошёлся бы.
			delete(idx, el)
			continue
		}
		idx[el] = kept
	}
	for i, iv := range after {
		idx[iv.Element] = append(idx[iv.Element], OccupationRef{Unit: unitID, Visit: i})
	}
	for _, iv := range after {
		sortRefs(idx[iv.Element])
	}
}

// sortRefs приводит список к каноническому порядку.
func sortRefs(refs []OccupationRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Unit != refs[j].Unit {
			return refs[i].Unit < refs[j].Unit
		}
		return refs[i].Visit < refs[j].Visit
	})
}

// RebuildOccupancy — пересобрать индекс с нуля по отрезкам всех тел.
//
// Существует РАДИ СТОРОЖА, а не ради работы: в бою индекс копится по одному
// телу (setOccupancy), и полный пересчёт не зовётся никогда. Сторож же без него
// невозможен — сравнивать накопленное не с чем.
//
// Обход идёт по m.Units, а не по карте Motions: порядок обхода карты в Go
// случаен, а канонический результат обязан быть одинаков от прогона к прогону.
func (m *Match) RebuildOccupancy() Occupancy {
	out := Occupancy{}
	for _, u := range m.Units {
		mo, ok := m.Motions[u.ID]
		if !ok {
			continue
		}
		for i, iv := range mo.Span {
			out[iv.Element] = append(out[iv.Element], OccupationRef{Unit: u.ID, Visit: i})
		}
	}
	for el := range out {
		sortRefs(out[el])
	}
	return out
}

// Conflict — накладывается ли отрезок на чей-то ещё.
//
// Своё тело пропускается по имени: тело, которое двигают, накладывается само на
// себя всегда — на то место, где оно только что стояло.
//
// Отдаёт ССЫЛКУ и МЕСТО: отказ обязан называть, кто и где, а не только «занято».
// Отказ, у которого нет места, заставляет искать его глазами по всей карте.
func (m Match) Conflict(unitID string, sp track.Span) (OccupationRef, netloc.IntervalS, bool) {
	for _, iv := range sp {
		for _, ref := range m.Occupied[iv.Element] {
			if ref.Unit == unitID {
				continue
			}
			busy, ok := ref.Interval(m)
			if !ok {
				continue
			}
			// Полуоткрытые интервалы: касание концами наложением НЕ считается
			// (разбор — в шапке track.Span).
			if iv.From < busy.To && busy.From < iv.To {
				return ref, netloc.IntervalS{
					Element: iv.Element,
					From:    max(iv.From, busy.From),
					To:      min(iv.To, busy.To),
				}, true
			}
		}
	}
	return OccupationRef{}, netloc.IntervalS{}, false
}

// OccupiedBy — кто стоит на элементе. Порядок канонический.
//
// Первый вопрос диспетчерской половины («можно ли перевести остряк») задаётся
// именно так: у устройства спрашивают его проходы, у прохода — занятость. Метод
// заведён вместе с индексом, потому что без него индекс пришлось бы читать
// полем, то есть отдать наружу право его править.
func (m Match) OccupiedBy(element string) []OccupationRef {
	return m.Occupied[element]
}

// DeviceBusy — занято ли устройство: стоит ли кто-нибудь хоть на одном из его
// проходов.
//
// Отдаёт имя тела, а не признак: «стрелка занята» без ответа «кем» — это отказ,
// после которого игрок ищет причину глазами.
func (m Match) DeviceBusy(net *track.CompiledNetwork, deviceID string) (string, bool) {
	dev, ok := net.Devices[deviceID]
	if !ok {
		return "", false
	}
	for _, tr := range dev.Traversals {
		for _, ref := range m.Occupied[tr.Passage] {
			return ref.Unit, true
		}
	}
	return "", false
}

// checkOccupancy — сторож инварианта: индекс совпадает с полным пересчётом.
//
// Не зовётся в бою: это проверка КОДА, а не данных, и её место в тесте. Живёт
// здесь, а не в тестовом файле, потому что сравнивать надо неэкспортированное
// поведение накопления, а не только его результат.
func (m *Match) checkOccupancy() error {
	want := m.RebuildOccupancy()
	got := m.occupancy()
	if len(want) != len(got) {
		return fmt.Errorf("match: занятость: элементов в индексе %d, при полном пересчёте %d",
			len(got), len(want))
	}
	for el, wantRefs := range want {
		gotRefs, ok := got[el]
		if !ok {
			return fmt.Errorf("match: занятость: элемента %s нет в индексе", el)
		}
		if len(gotRefs) != len(wantRefs) {
			return fmt.Errorf("match: занятость элемента %s: ссылок %d, при полном пересчёте %d",
				el, len(gotRefs), len(wantRefs))
		}
		for i := range wantRefs {
			if gotRefs[i] != wantRefs[i] {
				return fmt.Errorf("match: занятость элемента %s, ссылка %d: %+v, при полном пересчёте %+v",
					el, i, gotRefs[i], wantRefs[i])
			}
		}
	}
	return nil
}
