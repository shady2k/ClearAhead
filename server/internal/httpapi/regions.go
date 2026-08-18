package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/mapstore"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// regionsRouter — корень /regions/: развод по подресурсам и больше ничего.
//
// Роутер не знает НИ ОДНОГО хранилища — он знает только формы адресов. Это
// прямое следствие правила «композиция живёт в main.go»: сеть лежит в mapstore,
// рельеф в worldstore, и склеить их вправе только тот, кто открыл оба. Дай
// роутеру хранилище — и он станет вторым местом композиции, а обработчик начнёт
// добывать чужие данные через соседний обработчик.
//
// Каждый подресурс разбирает свой адрес ЗАНОВО и целиком: роутер выбирает
// ветку, а не проверяет её. Так подручка остаётся самостоятельной — её можно
// поднять отдельно (что и делают тесты), и она не зависит от того, кто её
// позвал.
type regionsRouter struct {
	manifest http.Handler
	network  http.Handler
	chunks   http.Handler
	objects  http.Handler
	live     http.Handler
	channel  http.Handler
}

// NewRegionsHandler собирает корень /regions/ из готовых подручек.
//
// Карта адресов:
//
//	GET /regions/{region}                            — манифест региона
//	GET /regions/{region}/revisions/{n}/network      — сеть региона (путь)
//	GET /regions/{region}/revisions/{n}/objects            — постройки региона
//	GET /regions/{region}/chunks/{level}/{cx}/{cz}         — рельеф, высоты
//	GET /regions/{region}/chunks/{level}/{cx}/{cz}/cover   — покров той же клетки
//	GET /regions/{region}/chunks/{level}/{cx}/{cz}/forest  — лес, только уровень 0
//	GET /regions/{region}/live                             — живое состояние партии
//	GET /regions/{region}/channel                          — канал команд (апгрейд в сокет)
//
// Канал стоит РЯДОМ с live, а не вместо него, и это временное состояние с
// названной ценой: одно и то же состояние партии отдаётся двумя способами.
// Ручка live остаётся, пока у неё есть читатели — проверки клиента ходят по
// HTTP и не умеют держать сокет. Уедет она вместе с ними, а не раньше: снести
// её сейчас значило бы ослепить проверки ради чистоты списка адресов.
//
// Ревизии в адресе live НЕТ, и это не забывчивость: ревизия называет карту, а
// живое состояние к карте не относится — оно принадлежит партии, идущей на этой
// карте. Поставь туда ревизию — и получилось бы, что положение локомотива есть
// свойство ревизии региона, то есть переставить машину можно только перерисовав
// мир.
//
// Корень один, и это регион. До биды ClearAhead-8kx их было два: сеть на
// /maps/{id}/revisions/{n}/geometry, рельеф на /regions/{id}/chunks/…, а
// связывало их соглашение `region := m.MapID` из одной строки
// worldgen.Bootstrap — то есть знание, которого у клиента нет и быть не должно.
func NewRegionsHandler(manifest, network, chunks, objects, live, channel http.Handler) http.Handler {
	return &regionsRouter{manifest: manifest, network: network, chunks: chunks,
		objects: objects, live: live, channel: channel}
}

func (h *regionsRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Адрес недоверен: чужая форма — 404 при любом методе. Отвечать 405 на
	// несуществующий ресурс значило бы подтверждать, что он есть.
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "regions" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	switch {
	case len(parts) == 2:
		h.manifest.ServeHTTP(w, r)
	case len(parts) == 5 && parts[2] == "revisions" && parts[4] == "network":
		h.network.ServeHTTP(w, r)
	case len(parts) == 5 && parts[2] == "revisions" && parts[4] == "objects":
		h.objects.ServeHTTP(w, r)
	case len(parts) == 3 && parts[2] == "live":
		h.live.ServeHTTP(w, r)
	case len(parts) == 3 && parts[2] == "channel":
		h.channel.ServeHTTP(w, r)
	case len(parts) == 6 && parts[2] == "chunks":
		h.chunks.ServeHTTP(w, r)
	// Покров — ХВОСТ адреса чанка, а не свой ресурс: клетка одна, и два пути к
	// ней развели бы её тождество надвое (разбор — в шапке chunkPath).
	case len(parts) == 7 && parts[2] == "chunks" && (parts[6] == "cover" || parts[6] == "forest"):
		h.chunks.ServeHTTP(w, r)
	default:
		http.NotFound(w, r)
	}
}

// regionManifest — то, без чего клиент не может сделать СЛЕДУЮЩИЙ запрос, и
// ничего сверх того.
//
// Манифест региона существует затем, чтобы для старта хватало одного адреса:
// клиент знает имя региона, спрашивает /regions/{region} и получает всё
// остальное. Каждое поле оправдано следующим запросом:
//
//   - region, epoch — что это за регион и какого он поколения; эпоха меняется,
//     когда мир пересобран целиком, и клиенту это повод выбросить кэш;
//   - revision — иначе НЕЧЕГО подставить в
//     /regions/{region}/revisions/{n}/network;
//   - network_model_hash, network_hash — проверка кэша до запроса тела;
//   - chunks — числа правила подробности, см. ниже;
//   - frame — геопривязка региона, если она у него есть.
//
// ЧЕГО ЗДЕСЬ НЕТ И НЕ БУДЕТ: перечня имеющихся чанков. Он устареет быстрее, чем
// доедет, как только появится режим строителя (ClearAhead-vo0): проложенный
// игроком путь порождает чанки в коридоре вокруг новой оси. Клиент держит сеть
// региона целиком (27 КБ) и выводит покрытие из неё сам — тем же правилом,
// числа которого лежат в поле chunks.
//
// Имена хешей (network_model_hash, network_hash) взяты у track.Manifest как
// есть: они называют ТЕ ЖЕ хеши, и вторая пара имён рядом с первой означала бы,
// что где-то придётся заводить таблицу соответствия.
//
// Хешей два и они не дублируют друг друга: network_model_hash считается от
// нормализованной модели сети, network_hash — от байтов тела /network. Клиенту
// нужны оба: по второму он решает, идти ли за телом, по первому — изменилась ли
// сама сеть, а не только её отрисовка.
type regionManifest struct {
	Region   string `json:"region"`
	Epoch    int64  `json:"epoch"`
	Revision int    `json:"revision"`

	NetworkModelHash string `json:"network_model_hash"`
	NetworkHash      string `json:"network_hash"`

	Chunks chunkRule `json:"chunks"`

	// Ground — правило земли: пороги, по которым покров и уклон превращаются в
	// вид земли. Разбор и сами числа — chunk.GroundRule.
	//
	// В МАНИФЕСТЕ, а не в чанке: правило одно на регион, а чанков тысячи, и
	// повторять его в каждом значило бы платить за него на каждой клетке. Тот же
	// довод, что у правила подробности рядом.
	//
	// Ради того же следующего запроса, что и всё остальное здесь: без порога
	// клиент не может нарисовать ПЕРВЫЙ же полученный чанк, не выдумав числа.
	Ground chunk.GroundRule `json:"ground"`

	// ProjectionHead — голова проекций региона: какая версия мира текущая и
	// из чего она собрана (журнал, сеть, рецепт). Без неё клиенту нечего
	// подставить в /worlds/{v}/... — ровно тот следующий запрос, ради
	// которого манифест и существует.
	ProjectionHead projectionHeadJSON `json:"projection_head"`

	// Domain — где мир кончается. Прямоугольник в координатах региона:
	// клиенту нужна эта граница ДО того, как он начнёт спрашивать чанки, — за
	// ней ответ всегда пустота (204), а не земля. Берётся у РЕГИОНА, а не у
	// карты, по той же причине, что и правило подробности: манифест называет
	// то, чем засеяна база.
	Domain mapfmt.Domain `json:"domain"`

	// Frame — блок georeference региона как есть, без перепаковки: значения
	// геопривязки меняют смысл координат, и вторая их запись рано или поздно
	// разошлась бы с первой. Отсутствует, если региону не задана привязка:
	// пустой объект в ответе клиент был бы обязан отличать от заданной привязки
	// с нулями, а это работа на пустом месте.
	Frame json.RawMessage `json:"frame,omitempty"`
}

// projectionHeadJSON — форма ProjectionHead на проводе (спека §6.3):
// четыре поля, отдельные НАРОЧНО — версия мира не растёт от журнала, и клиент
// обязан видеть их по отдельности, а не один счётчик «новизны».
type projectionHeadJSON struct {
	WorldVersion     int64  `json:"world_version"`
	SourceJournalSeq int64  `json:"source_journal_seq"`
	NetworkVersion   int64  `json:"network_version"`
	RegionRecipeHash string `json:"region_recipe_hash"`
}

// chunkRule — числа правила подробности: форма чанка из пакета chunk, охват из
// записи региона.
//
// Это не украшение манифеста: клиент обязан САМ вычислять, какой уровень чанка
// спрашивать в данной точке, — по расстоянию до ближайшей оси пути, как это
// делает chunk.Rule.LevelFor. Правило: уровень 0 покрывает Level0RadiusM от
// оси, каждый следующий — вдвое дальше, круги ВЛОЖЕНЫ, после MaxLevel не
// хранится ничего.
//
// Источников у чисел два, и это не небрежность: side_m, step_m и samples —
// форма чанка, одна на весь сервер; level0_radius_m и max_level — ОХВАТ, а он с
// 2026-08-12 свойство карты и потому свойство региона (chunk.Rule). Клиент
// разницы не видит и видеть не должен: он читает шесть чисел и не держит ни
// одного своего.
//
// Числа отдаются, а не зашиваются в клиент, потому что зашитая копия молча
// разойдётся с сервером при первой же смене геометрии сетки, и разойдётся не
// отказом, а неверными запросами: клиент пойдёт за уровнем, которого нет, и
// получит сплошные 204.
//
// # Что клиент обязан делать с этими числами
//
// Спрашивать уровень LevelFor(расстояние) В ТОЧКЕ и рисовать его один. Правило
// отбора КЛЕТОК у сервера своё (по телу клетки, worldgen.selector.covers), и
// повторять его клиент не должен: между «какие клетки хранятся» и «каким
// уровнем накрыта точка» разница как раз в том, что уровни перекрываются.
// Клиент, нарисовавший все доступные уровни, получит четыре поверхности разной
// подробности в одном месте — z-fighting вместо земли.
//
// # MAX_LEVEL — ПОТОЛОК, А НЕ ПРЕДПИСАНИЕ
//
// Это поле отвечает на вопрос «докуда мир ЗАСЕЯН», и только на него. Оно НЕ
// говорит клиенту, сколько уровней спрашивать: как далеко смотреть — свойство
// ВИДА, и решает его клиент своей настройкой (client/scripts/app.gd,
// ChunkRule.set_view_reach), как решает углы камеры и плотность травы. Клиент
// вправе спросить меньше — и с 2026-08-12 по умолчанию так и делает.
//
// Разделено это в тот день, когда стало видно, что одно число отвечает на два
// разных вопроса: «что сервер хранит» (предел, наш) и «как далеко видно»
// (дальность взгляда, клиентская). Слитые, они означали бы, что размер мира
// на диске управляет картинкой игрока, а смена настроек игрока требует другой
// карты — ровно так и появилась вторая карта репозитория, снесённая тем же
// вечером (разбор — у mapfmt.ShippedMapPath).
//
// Спросить БОЛЬШЕ по-прежнему нельзя: уровень выше max_level — 404, и это не
// вежливость, а тот же закон, что и всюду. Чего не порождено, того не отдать.
//
// Пустой ответ (204) на названном уровне означает «спускайся к более грубому».
// Это не обработка ошибки, а часть правила: расстояние клиент считает по СВОЕЙ
// выборке оси, и на границе круга его уровень вправе разойтись с серверным.
// Цена расхождения замерена (worldgen,
// TestClientLevelChoiceToleratesDistanceError): ошибка в половину шага выборки
// не стоит земли вовсе и стоит 0.53 % площади вблизи пути, нарисованной на
// уровень грубее должного.
type chunkRule struct {
	SideM         int     `json:"side_m"`
	StepM         int     `json:"step_m"`
	Samples       int     `json:"samples"`
	Level0RadiusM float64 `json:"level0_radius_m"`
	MaxLevel      int     `json:"max_level"`
	// AxisStepM — шаг, которым сервер выбирает точки оси пути, прежде чем
	// мерить до неё расстояние.
	//
	// Поля не было, и это была вторая половина дефекта ClearAhead-cue: правило
	// меряет расстояние до ТОЧЕК оси, выбранных с шагом 5 м, а манифест об этом
	// молчал. Клиент шаг угадал и попал, но угадывание — случайная удача, а не
	// контракт: числа правила либо отдаются все, либо клиент зашивает копию,
	// ради отсутствия которой поле chunks и существует.
	//
	// Число берётся из terrain, где ось выбирается на самом деле, а не из chunk,
	// где живут остальные числа контракта: копия в chunk однажды разошлась бы с
	// действительным шагом выборки, и разошлась бы молча.
	AxisStepM float64 `json:"axis_step_m"`
}

// regionAPI — манифест региона. ЕДИНСТВЕННОЕ место, где сходятся оба
// хранилища, и сходятся они здесь потому, что сам ресурс их сводит: регион и
// его эпоха живут в worldstore, ревизия и хеши сети — в mapstore. Оба пришли
// аргументами из main.go; ни одно не добывается через соседний обработчик.
type regionAPI struct {
	world *worldstore.Store
	maps  *mapstore.Store
}

// NewRegionManifestHandler собирает ручку манифеста региона.
//
// Маршрут: GET /regions/{region}
func NewRegionManifestHandler(world *worldstore.Store, maps *mapstore.Store) http.Handler {
	return &regionAPI{world: world, maps: maps}
}

func (a *regionAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	region, ok := regionPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "только GET и HEAD", http.StatusMethodNotAllowed)
		return
	}

	reg, ok, err := a.world.GetRegion(region)
	if err != nil {
		http.Error(w, "хранилище недоступно", http.StatusInternalServerError)
		return
	}
	if !ok {
		// Несуществующий регион — 404. В отличие от чанка, у которого пустота
		// внутри существующего региона законна (204), сам регион либо есть,
		// либо это опечатка в адресе.
		http.NotFound(w, r)
		return
	}

	// Сеть региона держит mapstore, и без неё манифест не выполняет своей
	// работы: ревизию подставить неоткуда, а ровно за ней клиент сюда и пришёл.
	// Пустой старт и чужая карта в памяти дают 404 — тот же ответ, что у
	// /manifest и у самой сети, а не половину манифеста, которую клиент был бы
	// обязан разбирать вторым состоянием.
	//
	// Цена названа: регион, у которого есть рельеф, но нет сети в памяти,
	// снаружи неотличим от опечатки в имени. Сегодня такого состояния не
	// возникает — main.go засевает mapstore и worldstore одной и той же
	// затравкой, — а когда регионов станет больше одного, различать придётся, и
	// различать по-настоящему: не полем в манифесте, а тем, что сеть переедет в
	// ту же базу, что и рельеф.
	st, ok := a.maps.Current()
	if !ok || st.Manifest.MapID != region {
		http.NotFound(w, r)
		return
	}
	// Голова проекций — часть манифеста: без версии мира клиенту нечего
	// подставить в /worlds/{v}/..., и манифест, её не назвавший, не выполняет
	// своей работы. Регион без головы — база прежней сборки без бутстрапа,
	// и это состояние поломки, а не законный ответ.
	head, ok, err := a.world.GetProjectionHead(reg.ID)
	if err != nil {
		http.Error(w, "хранилище недоступно", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "голова проекций не заведена", http.StatusInternalServerError)
		return
	}

	man := regionManifest{
		Region:           reg.ID,
		Epoch:            reg.Epoch,
		Revision:         st.Manifest.Revision,
		NetworkModelHash: st.Manifest.NetworkModelHash,
		NetworkHash:      st.Manifest.NetworkHash,
		ProjectionHead: projectionHeadJSON{
			WorldVersion:     head.WorldVersion,
			SourceJournalSeq: head.SourceJournalSeq,
			NetworkVersion:   head.NetworkVersion,
			RegionRecipeHash: head.RegionRecipeHash,
		},
		Chunks: chunkRule{
			SideM:   chunk.SideM0,
			StepM:   chunk.StepM0,
			Samples: chunk.Samples,
			// Охват берётся У РЕГИОНА, а не у пакета chunk и не у карты: клиенту
			// нужно правило ТОГО, ЧТО ЛЕЖИТ В БАЗЕ. Файл карты на диске мог
			// смениться после засева, и назвать клиенту его числа значило бы
			// обещать землю, которой в базе нет.
			Level0RadiusM: reg.Rule.Level0RadiusM,
			MaxLevel:      reg.Rule.MaxLevel,
			AxisStepM:     terrain.AxisStepM,
		},
		Ground: chunk.Ground,
		Domain: reg.Domain,
		Frame:  frameJSON(reg.Frame),
	}
	body, err := json.Marshal(man)
	if err != nil {
		// Сюда попадает ровно один случай: frame в базе не является JSON.
		// Пропускать такой регион молча нельзя — клиент получил бы битый ответ.
		http.Error(w, "манифест региона не собирается", http.StatusInternalServerError)
		return
	}

	// ETag считается от ОТДАННЫХ БАЙТ, а не от одного из вложенных хешей:
	// манифест несёт и эпоху, и геопривязку, и числа правила подробности, а
	// network_hash про них ничего не знает и на их изменение не
	// откликнулся бы.
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`

	// Кэш — no-cache, как у чанков и у манифеста карты, и по той же причине:
	// адрес не называет версию, значит обещать неизменность нечем. immutable
	// (RFC 8246) здесь был бы ложью ценой в навсегда устаревшую ревизию у того,
	// кто манифест уже загрузил: клиент, которому сказали immutable, не
	// предъявит If-None-Match, и ETag станет некому показать. no-cache — это не
	// no-store: копию хранить можно, отдавать её без проверки нельзя, и дешёвый
	// 304 остаётся на месте.
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, no-cache")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(body)
}

// frameJSON отдаёт блок геопривязки региона или ничего.
//
// В базе поле не пустует: worldgen пишет туда "{}", когда привязки у карты нет.
// Пустой объект в ответе — та же пустота, только клиент обязан её разбирать,
// поэтому наружу он не идёт.
func frameJSON(frame string) json.RawMessage {
	f := strings.TrimSpace(frame)
	if f == "" || f == "{}" {
		return nil
	}
	return json.RawMessage(f)
}

// regionPath разбирает /regions/{region}.
//
// Проверяется только форма адреса — то, при чём ответ обязан быть 404: лишние
// или недостающие сегменты, пустое имя. Существование региона — не форма, и
// решается оно выше.
func regionPath(p string) (string, bool) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 2 || parts[0] != "regions" || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}
