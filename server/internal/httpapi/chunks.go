package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// HeaderChunkBaseZ — опорная высота чанка, целые миллиметры, десятичное число
// со знаком.
//
// Заголовок обязателен в каждом успешном ответе и не является служебной
// мелочью: тело несёт int16 сантиметров ОТНОСИТЕЛЬНО base_z (контракт чанков
// §5), и без базы отсчёты не значат ничего. В тело база не кладётся сознательно
// — блоб остаётся ровно chunk.HeightsBytes байт и одинаковым на любом уровне,
// а клиент выделяет буфер один раз.
//
// Единица — миллиметр, а не метр: в хранилище база лежит целым числом
// миллиметров (worldstore.Chunk.BaseZmm) именно затем, чтобы ни сравнение, ни
// хеш не зависели от представления float. Печатать её дробью значило бы вернуть
// на провод ту самую неопределённость, ради ухода от которой она целая.
const HeaderChunkBaseZ = "X-Chunk-Base-Z-Mm"

// chunksAPI — отдача статического яруса чанков.
//
// Хранилище приходит аргументом, а не берётся из глобала: обработчиков может
// оказаться несколько (тест поднимает свою базу во временном каталоге), и
// общий изменяемый пакетный уровень тут не нужен ничему.
type chunksAPI struct{ store *worldstore.Store }

// NewChunksHandler собирает ручку чанков над открытой базой мира.
//
// Маршрут: GET /regions/{region}/chunks/{level}/{cx}/{cz}
//
// Ключ чанка — (region, level, cx, cz) (world-storage §4), и путь повторяет его
// сегмент в сегмент: адрес чанка виден в URL целиком, без скрытых умолчаний.
func NewChunksHandler(store *worldstore.Store) http.Handler {
	return &chunksAPI{store: store}
}

func (a *chunksAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Разбор пути идёт первым: путь приходит снаружи и недоверен, поэтому
	// сначала выясняется, адресован ли вообще чанк, и лишь потом — можно ли
	// его так спрашивать. Синтаксически чужой путь — это «нет такого ресурса»
	// при любом методе.
	addr, ok := chunkPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// HEAD обязателен наравне с GET: RFC 9110 требует его от сервера общего
	// назначения, и на нём держится проверка кэша прокси и клиентов. Тело для
	// HEAD подавляет net/http сам, поэтому дальше ветвление не нужно — но
	// Content-Length приходится выставить руками, иначе на HEAD он не доедет.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "только GET и HEAD", http.StatusMethodNotAllowed)
		return
	}

	// Существование региона проверяется отдельно от наличия чанка, и это
	// главное различение всей ручки (контракт чанков §6): опечатка в имени
	// региона — неверный адрес, а пустота внутри существующего региона —
	// законное состояние разреженного хранилища. Клиент обязан вести себя
	// по-разному, значит и ответы обязаны различаться.
	if _, ok, err := a.store.GetRegion(addr.Region); err != nil {
		http.Error(w, "хранилище недоступно", http.StatusInternalServerError)
		return
	} else if !ok {
		http.NotFound(w, r)
		return
	}

	c, ok, err := a.store.GetChunk(addr)
	if err != nil {
		http.Error(w, "хранилище недоступно", http.StatusInternalServerError)
		return
	}
	if !ok {
		// 204, а НЕ 404. Отсутствующий чанк не хранится и не является сбоем:
		// «ресурс адресован верно, содержимого нет — рисуй базовую
		// поверхность». 404 здесь был бы неотличим от опечатки в имени
		// региона, а опечатка и пустота требуют от клиента разного.
		// Края мира как границы не существует — есть область, где чанков нет.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Блоб собирается кодеком пакета chunk, а не здесь: порядок байт
	// (little-endian) и порядок обхода (строками по возрастанию j, внутри
	// строки по возрастанию i) — часть контракта, и вторая их реализация рано
	// или поздно разошлась бы с первой.
	blob, err := chunk.EncodeHeights(c.Heights)
	if err != nil {
		http.Error(w, "чанк повреждён", http.StatusInternalServerError)
		return
	}

	// ETag — хеш чанка из хранилища; он считается там же, где пишется тело
	// (worldstore.PutChunk), поэтому не может описать чужие байты.
	//
	// Кэш — no-cache, и это не описка вместо no-store: no-cache разрешает
	// хранить копию и запрещает отдавать её без проверки, no-store запретил бы
	// хранить вовсе и убил бы дешёвый 304 вместе с ним. Родственная запись
	// «max-age=0, must-revalidate» значит то же самое, но двумя токенами и с
	// оговорками у старых прокси; здесь нужна ровно одна мысль — «кэшируй, но
	// всегда спрашивай», — и её выражает no-cache. Такой же выбор и по той же
	// причине уже сделан у манифеста карты (api.go) и у манифеста региона
	// (regions.go).
	//
	// immutable (RFC 8246) здесь был бы ложью, и ценой ей — навсегда устаревший
	// рельеф у того, кто чанк уже загрузил. immutable означает «год не шли
	// условный запрос»: клиент, которому так сказали, не предъявит ни
	// If-None-Match, ни что-либо ещё, и ETag становится некому показать.
	// Обещать неизменность вправе только адрес, называющий версию, а этот её не
	// называет: (region, level, cx, cz) — место, а не состояние места.
	// Содержимое же обязано меняться: земляные работы запечены в высоты, и
	// проложенный игроком путь переписывает чанки в коридоре вокруг новой оси.
	//
	// Ревизия у чанка в базе есть (worldstore.Chunk.Revision), наружу она не
	// выходит. Ход второй — вывести её в URL, как уже сделано у сети региона
	// (/regions/{region}/revisions/{n}/network), и тогда immutable перестанет
	// врать.
	// Делать это до появления самих мутаций рано: версия в адресе — контракт с
	// клиентом, и вводить её стоит вместе с тем, что её меняет.
	etag := `"` + c.Hash + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, no-cache")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set(HeaderChunkBaseZ, strconv.FormatInt(c.BaseZmm, 10))
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(blob)))
	w.Write(blob)
}

// chunkPath разбирает /regions/{region}/chunks/{level}/{cx}/{cz}.
//
// Здесь проверяется только форма адреса — то, при чём ответ обязан быть 404:
// лишние или недостающие сегменты, нечисловые и переполняющие координаты,
// уровень вне хранимого диапазона. Существование региона и наличие чанка — не
// форма, и решаются они выше.
//
// Ничего не паникует и ничего не предполагает о входе: путь приходит снаружи.
func chunkPath(p string) (chunk.Address, bool) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 6 || parts[0] != "regions" || parts[2] != "chunks" {
		return chunk.Address{}, false
	}
	region := parts[1]
	if region == "" {
		return chunk.Address{}, false
	}
	level, ok := parseInt32(parts[3])
	if !ok || level < 0 || level > chunk.MaxLevel {
		// Уровень вне [0, MaxLevel] — неверный адрес, а не пустота: такого
		// уровня подробности не существует ни у одного региона, и отвечать на
		// него 204 значило бы обещать, что чанк там когда-нибудь появится.
		return chunk.Address{}, false
	}
	cx, ok := parseInt32(parts[4])
	if !ok {
		return chunk.Address{}, false
	}
	cz, ok := parseInt32(parts[5])
	if !ok {
		return chunk.Address{}, false
	}
	return chunk.Address{Region: region, Level: level, CX: cx, CZ: cz}, true
}

// parseInt32 разбирает десятичное целое, помещающееся в int32.
//
// Ширина ограничена сознательно: на стороне 256 м int32 покрывает координату
// чанка до полумиллиарда километров, а всё, что не влезло, — заведомо мусор в
// пути, а не адрес далёкого угла мира. Переполнение обязано стать 404, а не
// молча завернуться.
func parseInt32(s string) (int, bool) {
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, false
	}
	return int(v), true
}
