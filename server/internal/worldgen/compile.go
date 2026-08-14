// Компиляция проекций региона: полная по сети, адресная по затронутому
// замыканию (ClearAhead-sqym.4, ClearAhead-vo0).
//
// Замыкание инвалидизации считает пакет project (project.Closure) — этот файл
// его ПОТРЕБИТЕЛЬ: по изменённому исходнику и его габариту влияния вычисляет,
// какие чанки пересобрать, пересобирает ровно их и публикует атомарно по
// группам согласованности. Правило §5.4 спеки соблюдается по построению:
// сначала замыкание ЦЕЛИКОМ, потом пересборка — «пересобираем и попутно
// выясняем, что ещё задето» оставило бы старую землю в адресе, обнаруженном
// последним.
//
// # Три категории проекций, и они не сливаются (решение координатора
// 2026-08-13, уточнено sqym.5)
//
//  1. Материализовано ЗДЕСЬ: высоты, покров, лес — строка чанка worldstore.
//     Их пересборка пишет строки по адресам замыкания (OutcomeRebuilt).
//  2. Материализовано ЗДЕСЬ ЖЕ: сеть. С ProjectionHead (sqym.5) атомарная
//     публикация группы «сеть + поверхность» достижима: сеть компилируется
//     пересборкой, и тело + строки поверхности ложатся ОДНОЙ транзакцией
//     (worldstore.Publish) под новую версию мира.
//  3. Выводится КЛИЕНТОМ, а не сервером: маска балласта и геометрия (§5.4
//     спеки). Серверной формы нет по РЕШЕНИЮ — адреса делят с поверхностью,
//     сервер описывает площадь высотами, клиент по ним же тесселирует
//     (OutcomeClientDerived).
//
// Вода — четвёртый исход (OutcomeNotMaterialized): проекция объявлена
// (project.declared), но почанковой формы ещё нет — гидрологию первая версия
// не реализует (§5.4). Перечень исходов прибит тестом
// (TestRebuildOutcomeCategories): материализовал кто-то воду почанково — тест
// падает и заставляет обновить и пересборку, и объявление.
package worldgen

import (
	"fmt"
	"math"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/project"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/track"
	"github.com/shady2k/ClearAhead/server/internal/vegetation"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// CompileNetwork — ПОЛНАЯ компиляция сети из исходников: один вызов, чистая
// функция.
//
// Возвращает нормализованную сеть и геометрию рендера — ровно те артефакты,
// которые mapstore кладёт в своё состояние (mapstore.buildState), то есть
// проекцию Network целиком: единственная проекция с адресом корня мира
// (project.WorldRoot), остальные пересобираются почанково. Сеть — чистая
// функция карты: правка пути перекомпилирует её целиком, и для этого не
// нужно ни состояния, ни журнала (контракт §1.2).
func CompileNetwork(m *mapfmt.Map) (*track.CompiledNetwork, *track.RenderGeometry, error) {
	if err := mapfmt.Validate(m); err != nil {
		return nil, nil, fmt.Errorf("worldgen: сеть: %w", err)
	}
	cn, rg, err := track.Compile(m)
	if err != nil {
		return nil, nil, fmt.Errorf("worldgen: сеть: %w", err)
	}
	return cn, rg, nil
}

// buildingClearM — радиус вырубки вокруг постройки. Число живёт в terrain
// (terrain.go:139, поле не экспортируется) и обязано быть названо здесь явно:
// замыкание режет вырубку ЗА габаритом пятна (project.SourceStructure), и две
// копии числа разошлись бы молча — лес пересобирался бы не там, где вырублен.
const buildingClearM = 30.0

// regionOf строит факты рецепта региона, которые читает замыкание, из карты.
//
// Правило подробности — из рельефа (то же, что кладёт Bootstrap в запись
// региона), реки — из объектов карты, вырубка — константа выше. План карты —
// X на восток, Y на север, Z — отметка; замыканию нужен только план, поэтому
// Y речной оси ложится в project.Point.Z.
func regionOf(m *mapfmt.Map) (project.Region, error) {
	rule, err := terrain.RuleOf(m)
	if err != nil {
		return project.Region{}, err
	}
	r := project.Region{
		Rule:           rule,
		BuildingClearM: buildingClearM,
	}
	if m.Objects != nil {
		for _, rv := range m.Objects.Rivers {
			axis := make([]project.Point, 0, len(rv.Axis))
			for _, p := range rv.Axis {
				axis = append(axis, project.Point{X: p.X, Z: p.Y})
			}
			r.Rivers = append(r.Rivers, project.River{
				ID:         rv.ID,
				Axis:       axis,
				HalfWidthM: rv.HalfWidthM,
				BankM:      rv.BankM,
				ValleyM:    rv.ValleyM,
			})
		}
	}
	return r, nil
}

// Outcome — исход пересборки одной проекции замыкания. Проекция не имеет
// права молча отсутствовать в отчёте: категория каждой названа, и перечень
// категорий прибит тестом.
type Outcome int

const (
	// OutcomeRebuilt — проекция материализована ЗДЕСЬ (строки чанка: высоты,
	// покров, лес; сеть — телом под новой версией мира) и пересобрана: каждый
	// адрес замыкания записан ровно один раз.
	OutcomeRebuilt Outcome = iota
	// OutcomeClientDerived — серверной формы нет ПО РЕШЕНИЮ: клиент выводит
	// проекцию из своей входной (маска балласта и геометрия, §5.4 спеки).
	// Пересобирать серверу нечего.
	OutcomeClientDerived
	// OutcomeNotMaterialized — проекция объявлена (project.declared), но
	// почанковой формы ещё нет: вода. Гидрологию первая версия не реализует
	// (§5.4); появление формы обязано обновить и пересборку, и тест-объявление.
	OutcomeNotMaterialized
)

// EntryResult — исход одной проекции замыкания.
type EntryResult struct {
	Projection project.Projection
	Outcome    Outcome
	// Rebuilt — сколько адресов пересобрано. Заполнен только для
	// OutcomeRebuilt: для остальных исходов адреса не пересобирались.
	Rebuilt int
	// Reason — почему проекция не пересобрана (все исходы кроме Rebuilt).
	Reason string
}

// GroupResult — исход группы согласованности: проекции группы и сколько строк
// записано её транзакцией.
type GroupResult struct {
	Group project.Group
	// Chunks — строк, записанных ТРАНЗАКЦИЕЙ ЭТОЙ группы. Адрес, вошедший в
	// несколько групп (поверхность и покров законно делят чанк), записан
	// первой группой и здесь не считается.
	Chunks  int
	Entries []EntryResult
}

// Result — полный отчёт пересборки: исход НА КАЖДУЮ проекцию замыкания.
type Result struct {
	Groups []GroupResult
	// TotalChunks — всего адресов пересобрано, по одному разу каждый.
	TotalChunks int
}

// groupRows — строки одной группы к записи и сама группа: единица
// пересборки. network — тело сети, если группа несёт проекцию Network: под
// версией мира оно публикуется той же транзакцией, что и строки (sqym.5).
type groupRows struct {
	group   project.Group
	rows    []worldstore.Chunk
	network []byte
}
type Compiler struct {
	store    *worldstore.Store
	region   string
	revision int64
	// journalSeq — позиция журнала, до которой построена эта пересборка.
	// Журнала команд ещё нет (engine.go: «журналировать нечего»), поэтому
	// сегодня это всегда 0; механизм заведён затем, что голова проекций
	// обязана называть команду, до которой построен мир, и версия мира при
	// этом НЕ растёт от журнала (спека §6.3).
	journalSeq int64
	// grading — правки высот региона: исходник рядом с картой, который
	// пересборка применяет к поверхности. Пустое множество — мир без
	// прямого терраморфинга, и чанки выходят байт в байт прежними.
	grading terrain.Grading
	// veg — источники растительности региона: лес пересборки — проекция
	// рецепта минус вырубка плюс посадка, иначе пересборка воскресила бы
	// срубленное (sqym.18).
	veg vegetation.Sources
}

// NewCompiler собирает компилятор для одного региона.
func NewCompiler(s *worldstore.Store, region string, revision, journalSeq int64) *Compiler {
	return NewCompilerGraded(s, region, revision, journalSeq, terrain.Grading{})
}

// NewCompilerGraded — компилятор над миром с правками высот: пересборка по
// замыканию SourceGrading обязана пересчитать чанки НАД правками, иначе
// правка, принятая коммитом, исчезла бы из опубликованного мира.
func NewCompilerGraded(s *worldstore.Store, region string, revision, journalSeq int64, g terrain.Grading) *Compiler {
	return NewCompilerSources(s, region, revision, journalSeq, Sources{Grading: g})
}

// NewCompilerSources — компилятор над миром с исходниками партии: правки
// высот и вырубка. Пересборка по замыканию SourceGrading обязана пересчитать
// чанки НАД правками, и лес — НАД вырубкой: иначе правка, принятая коммитом,
// исчезла бы из опубликованного мира, а срубленное дерево воскресло (sqym.18).
func NewCompilerSources(s *worldstore.Store, region string, revision, journalSeq int64, src Sources) *Compiler {
	return &Compiler{store: s, region: region, revision: revision, journalSeq: journalSeq, grading: src.Grading, veg: src.Vegetation}
}

// Compile — ПОЛНАЯ компиляция проекций региона из исходников (карты).
//
// ЧИСТАЯ ФУНКЦИЯ: строки чанков возвращаются, в базу ничего не пишется.
// Полный проход — эталон для адресной пересборки: тот же инвариант, что
// держит prepare (прогрев и порождение по требованию байт в байт равны),
// переносится на Compile — чанк, пересобранный адресно, обязан совпасть с
// чанком полного прохода (TestRebuildMatchesFullCompile).
func (c *Compiler) Compile(m *mapfmt.Map) ([]worldstore.Chunk, error) {
	sel, err := prepare(c.store, m, c.region, c.grading)
	if err != nil {
		return nil, err
	}
	baseZmm := int64(math.Round(sel.field.BaseZ() * 1000))
	var out []worldstore.Chunk
	_, _, err = walk(sel, c.region, sel.covers, func(a chunk.Address) error {
		ch, err := chunkAt(sel.field, baseZmm, a, c.revision, 0, c.veg)
		if err != nil {
			return err
		}
		out = append(out, ch)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Preview — предпросмотр правки: то же замыкание и та же пересборка, что у
// Rebuild, но БЕЗ записи — ноль строк в базу (замер TestPreviewWritesNothing).
//
// Разделение обязательно (спека §8.1): предпросмотр — призрачная геометрия
// без пересборки, авторитетный пересчёт — по подтверждению. 300 мс
// пересборки приемлемы для подтверждённой постройки и неприемлемы для
// перетаскивания мышью.
func (c *Compiler) Preview(m *mapfmt.Map, ch project.Change) (*Result, error) {
	res, _, err := c.plan(m, ch)
	return res, err
}

// Rebuild — авторитетный пересчёт: замыкание ЦЕЛИКОМ, потом пересборка ровно
// этих адресов и публикация ОДНОЙ ТРАНЗАКЦИЕЙ под новой версией мира.
//
// Каждый адрес замыкания пишется РОВНО ОДИН РАЗ: две записи одного адреса в
// одной пересборке — это два писателя, а пересборка не должна плодить
// писателей (гонка старого и нового порождения, спека §8.2 п. 2). Содержимое
// строки при этом ПОЛНОЕ — высоты, покров, лес: слой, вошедший в группу позже,
// едет со строкой первой группы, и читатель никогда не видит строку с новым
// покровом и старым лесом.
//
// Версия ОДНА на всю пересборку (спека §6.3 «мир публикуется одной версией»):
// читатель головы не должен увидеть состояние, где группа 1 уже вышла, а
// группа 2 ещё нет. Группы согласованности остаются единицей ПЕРЕСЧЁТА
// (шовный инвариант держится тем, что соседи по шву считаются в одной
// группе), но ложатся одной транзакцией.
func (c *Compiler) Rebuild(m *mapfmt.Map, ch project.Change) (*Result, error) {
	res, groups, err := c.plan(m, ch)
	if err != nil {
		return nil, err
	}
	var rows []worldstore.Chunk
	var network []byte
	for _, g := range groups {
		rows = append(rows, g.rows...)
		if g.network != nil {
			network = g.network
		}
	}
	if len(rows) == 0 && network == nil {
		// Замыкание без материализуемых проекций (например, только геометрия)
		// — согласованный no-op, а не пустая публикация: версия мира не растёт
		// от пересборки, которой нечего опубликовать.
		return res, nil
	}
	if _, err := c.store.Publish(c.region, rows, network, c.journalSeq); err != nil {
		return nil, err
	}
	return res, nil
}

// plan считает замыкание и пересборку адресов, но НЕ пишет: общий шаг
// Preview и Rebuild. Всё считается ДО первой записи — сбой счёта не оставляет
// наполовину опубликованную правку.
func (c *Compiler) plan(m *mapfmt.Map, ch project.Change) (*Result, []groupRows, error) {
	// Полный путь входа по карте С ПРАВКОЙ: валидация, распространение поз,
	// поле; сверка правила и домена с записью региона (prepare). Пересборка
	// не доверяет вызывающему — та же дисциплина, что у Generate. Правки
	// высот компилятор несёт с собой (NewCompilerGraded): источник живёт
	// рядом с компилятором, а не в карте.
	sel, err := prepare(c.store, m, c.region, c.grading)
	if err != nil {
		return nil, nil, err
	}
	reg, err := regionOf(m)
	if err != nil {
		return nil, nil, err
	}
	// ЗАМЫКАНИЕ ЦЕЛИКОМ, до пересборки хоть одного адреса (§5.4 спеки).
	closure, err := reg.Closure(ch)
	if err != nil {
		return nil, nil, err
	}

	baseZmm := int64(math.Round(sel.field.BaseZ() * 1000))
	res := &Result{}
	// groups — пачки строк по группам к пересборке: Rebuild собирает их в одну
	// транзакцию (см. Rebuild), Preview не пишет вовсе.
	var groups []groupRows
	// computed — посчитанные строки по адресу: адрес, вошедший в несколько
	// проекций (покров и лес одного следа), считается один раз.
	computed := make(map[chunk.Address]worldstore.Chunk)
	// written — адреса, уже отданные предыдущей группе: второй раз адрес не
	// пишется ни в какую группу.
	written := make(map[chunk.Address]bool)
	// networkBody — тело сети, скомпилированное ОДИН раз на замыкание: сеть
	// региона — единая проекция, и компилировать её по группе значило бы
	// получить N тел одной сети.
	var networkBody []byte

	// Замыкание регион-агностично: адрес проекта несёт (level, cx, cz) без
	// региона намеренно (project.Address), и потребитель подставляет свой.
	// Без этого шага строки ушли бы в базу с пустым регионом и упали на
	// внешнем ключе.
	region := c.region

	for _, plan := range closure.Groups {
		gr := GroupResult{Group: plan.Group}
		var rows []worldstore.Chunk
		var groupNetwork []byte
		for _, entry := range plan.Entries {
			er := EntryResult{Projection: entry.Projection}
			switch entry.Projection {
			case project.Network:
				// Сеть — ПРОЕКЦИЯ ЗДЕСЬ (sqym.5): с ProjectionHead атомарная
				// публикация группы «сеть + поверхность» достижима, и
				// OutcomeOtherWriter ушёл. Тело едет под новой версией мира
				// той же транзакцией, что и строки поверхности (Rebuild).
				er.Outcome = OutcomeRebuilt
				// Корень мира — один адрес публикации (project.WorldRoot).
				er.Rebuilt = len(entry.Addresses)
				if networkBody == nil {
					_, rg, err := CompileNetwork(m)
					if err != nil {
						return nil, nil, err
					}
					networkBody, err = track.RenderBody(rg)
					if err != nil {
						return nil, nil, err
					}
				}
				groupNetwork = networkBody
			case project.Geometry:
				er.Outcome = OutcomeClientDerived
				er.Reason = "геометрия выводится клиентом из высот и сети (§5.4): адреса делят с поверхностью, пересобирать серверу нечего"
			case project.Water:
				er.Outcome = OutcomeNotMaterialized
				er.Reason = "почанковой формы воды нет: гидрологию первая версия не реализует (§5.4)"
			default:
				er.Outcome = OutcomeRebuilt
				// Счёт проекции — ВСЕ её адреса замыкания, включая разделённые
				// с другой проекцией: строка такого адреса пересобрана целиком
				// (высоты, покров, лес) и записана первой группой. gr.Chunks
				// ниже считает строки транзакции ЭТОЙ группы, и адрес,
				// ушедший с более ранней группой, в неё не попадает.
				for _, a := range entry.Addresses {
					// Адрес замыкания региона не несёт (project.Address):
					// подстановка региона — шаг потребителя, и он здесь.
					a.Region = region
					if written[a] {
						continue
					}
					written[a] = true
					ch, ok := computed[a]
					if !ok {
						// Версию строке назначает Publish: план считает её ДО
						// транзакции, и названная здесь версия устарела бы к
						// моменту публикации (две пересборки подряд).
						ch, err = chunkAt(sel.field, baseZmm, a, c.revision, 0, c.veg)
						if err != nil {
							return nil, nil, err
						}
						computed[a] = ch
					}
					rows = append(rows, ch)
					gr.Chunks++
				}
				er.Rebuilt = len(entry.Addresses)
			}
			gr.Entries = append(gr.Entries, er)
		}
		if len(gr.Entries) > 0 {
			res.Groups = append(res.Groups, gr)
		}
		if len(rows) > 0 || groupNetwork != nil {
			groups = append(groups, groupRows{group: plan.Group, rows: rows, network: groupNetwork})
		}
	}
	res.TotalChunks = len(written)
	return res, groups, nil
}
