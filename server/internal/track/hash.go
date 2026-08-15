package track

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// Manifest связывает ревизию карты с двумя хешами: модели сети и отдаваемого
// ресурса network. Своего ресурса у первого нет и не будет — он называет
// артефакт компиляции, а не то, что можно спросить по адресу. Пара
// (MapID, Revision) определяет ровно один манифест — иначе immutable-URL лжёт.
type Manifest struct {
	MapID    string `json:"map_id"`
	Revision int    `json:"map_revision"`

	// NetworkModelHash — хеш НОРМАЛИЗОВАННОЙ МОДЕЛИ сети (CompiledNetwork):
	// элементы с концами, длинами и профилями, устройства с портами и
	// переходами, сооружения, плюс геопривязка. Это вход физики и
	// безопасности; координат в нём нет.
	//
	// Звался track_hash. Имя называло ВИД — ровно то, за что ресурс пути
	// переименован в network (ClearAhead-8kx): ресурс называет КЛАСС
	// содержимого, а с приездом автомобильных дорог дорожный элемент лежал бы
	// под хешем со словом «рельсы» в имени. Переименовано сейчас по доводу шва 6
	// (ClearAhead-4he, id.go): клиента нет, карт мало и они свои — сегодня это
	// правка строки, через ревизию-другую миграция.
	//
	// ЧЕМ ОТЛИЧАЕТСЯ ОТ NetworkHash, раз оба про сеть: этот считается от МОДЕЛИ,
	// тот — от БАЙТОВ ответа. Они расходятся В ОБЕ СТОРОНЫ, и это не выдумка про
	// будущее, а сегодняшняя карта: геопривязка меняет модель, а в теле её нет
	// вовсе (TestManifestChangesOnGeoreference); полуширина балласта меняет тело,
	// а в выжимку модели не входит — рисованию она нужна, физике нет. Слово model
	// в имени и есть эта разница.
	//
	// ОТВЕРГНУТО: topology_hash — в хеш входят длины, профили и геопривязка, а
	// имя geometry_hash спека отбросила за симметричную ложь, «раз в хеш входит
	// и топология» (map-format-design §5); infrastructure_hash — второе слово
	// для класса, у которого уже есть слово network, то есть та самая пара имён
	// одной вещи, которую проект только что разбирал; model_hash — рядом встанут
	// terrain_hash и visual_hash, у которых модель тоже своя, и «модель чего»
	// пришлось бы спрашивать каждый раз.
	NetworkModelHash string `json:"network_model_hash"`

	// NetworkHash — хеш ресурса network. Звался render_geometry_hash, пока
	// существовал ресурс geometry; ресурса больше нет, а хеш считается ровно от
	// байтов тела network, и двух имён одной вещи проект не держит.
	//
	// Переименовано И ВНУТРИ, а не только на проводе: разведённые имена снаружи
	// и внутри означают таблицу соответствия, которую придётся держать в голове
	// каждому, кто ходит между hash.go и ручкой.
	NetworkHash string `json:"network_hash"`
}

// BuildManifest считает хеши по нормализованной внутренней модели, а не по
// исходному JSON. Так снимается весь класс вопросов про каноническую
// сериализацию: порядок ключей, форма чисел, -0, экспоненты, Unicode.
func BuildManifest(m *mapfmt.Map, cn *CompiledNetwork, rg *RenderGeometry) (Manifest, error) {
	mh := sha256.New()
	writeNetworkModel(mh, m, cn)

	// NetworkHash считается по ТЕМ САМЫМ БАЙТАМ, которые уедут клиенту,
	// а не по рукописной модели рядом с ними.
	//
	// Первая редакция писала свою текстовую выжимку и не включала в неё Slope,
	// хотя он сериализуется в JSON. Тело менялось, хеш — нет, и клиент с
	// Cache-Control: immutable получал 304 и НАВСЕГДА сохранял устаревшую
	// геометрию. Ошибка не давала отказа: сервер стартовал, всё выглядело
	// исправным.
	//
	// Пока хешируется выжимка, любое новое поле провода надо не забыть добавить
	// и туда. Забудут. Байты ответа забыть нельзя: они и есть ответ.
	body, err := renderBody(rg)
	if err != nil {
		return Manifest{}, err
	}
	rh := sha256.Sum256(body)
	return Manifest{
		MapID:            m.MapID,
		Revision:         m.MapRevision,
		NetworkModelHash: hex.EncodeToString(mh.Sum(nil)),
		NetworkHash:      hex.EncodeToString(rh[:]),
	}, nil
}

func writeNetworkModel(w io.Writer, m *mapfmt.Map, cn *CompiledNetwork) {
	fmt.Fprintf(w, "v%d|%s|%d\n", mapfmt.FormatVersion, cn.MapID, cn.Revision)

	// Геопривязка входит: она меняет смысл координат. Provenance не входит:
	// правка комментария не должна сбрасывать кэш клиента.
	if g := m.Georeference; g != nil {
		fmt.Fprintf(w, "geo|%s|%.12g|%.12g|%.12g|%s|%.12g|%.12g\n",
			g.Datum, g.Origin.Lat, g.Origin.Lon, g.Origin.H,
			g.OriginHeightKind, g.XAxisAzimuthDeg, g.GroundToGrid)
	}

	ids := make([]string, 0, len(cn.Elements))
	for id := range cn.Elements {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := cn.Elements[id]
		// Kind входит в выжимку: вид пути — факт О САМОЙ МОДЕЛИ, а не о её
		// отрисовке, и физика с безопасностью первыми же и разойдутся, если
		// рельсовый элемент подменить дорожным без смены network_model_hash. Это
		// ровно то поле, которое предсказано абзацем «забудут» выше: в тело
		// network оно попадает само (там хешируются байты), а сюда его надо
		// вписать рукой. Тест TestNetworkModelHashCoversElementKind падает, если
		// его убрать.
		fmt.Fprintf(w, "el|%s|%s|%s|%s|%d|%d\n", e.ID, e.Kind, e.From, e.To, int64(e.LengthU), int64(e.LengthS))
		for _, seg := range e.Prof {
			fmt.Fprintf(w, "  pr|%d|%.12g|%.12g\n", int64(seg.LengthU), seg.StartSlope, seg.EndSlope)
		}
	}

	// Порты и переходы сериализуются поимённо и по порядку: их число не
	// фиксировано, поэтому позиционная запись «общий, прямой, боковой» больше
	// невозможна. Порядок задан Passages() и PortIDs() и потому детерминирован.
	tids := make([]string, 0, len(cn.Devices))
	for id := range cn.Devices {
		tids = append(tids, id)
	}
	sort.Strings(tids)
	for _, id := range tids {
		writeDevice(w, cn.Devices[id])
	}

	// Префикс сменён с ts| на st| вместе с переименованием trackside ->
	// structures (2026-08-11), и это осознанная смена ЗНАЧЕНИЯ хеша, а не
	// косметика. Довод: NetworkHash этой же ревизии меняется в любом случае —
	// json-тег в теле стал "structures", а тот хеш считается от байтов ответа.
	// Раз манифест ревизии всё равно другой, беречь NetworkModelHash не от чего,
	// и выбор сводится к «оставить в выжимке мёртвое слово, которое никто не
	// грепнет» против «одно имя во всех трёх местах». Хеши нигде не записаны:
	// манифест собирается в памяти при укладке карты (mapstore.Put), золотых
	// значений в тестах нет, карты строятся кодом. Цена смены — ноль сегодня и
	// миграция после первой авторской карты, то есть ровно шов 6 (ClearAhead-4he).
	oids := make([]string, 0, len(cn.Structures))
	for id := range cn.Structures {
		oids = append(oids, id)
	}
	sort.Strings(oids)
	for _, id := range oids {
		for _, sp := range cn.Structures[id] {
			fmt.Fprintf(w, "st|%s|%s|%d|%d|%s\n", id, sp.Element, int64(sp.From), int64(sp.To), sp.Direction)
		}
	}
}

// RenderBody сериализует геометрию ровно так, как её отдаёт ручка.
//
// Единственное место сериализации провода: и хеш, и HTTP-ответ обязаны брать
// байты отсюда, иначе они разойдутся, а ETag начнёт лгать.
func RenderBody(rg *RenderGeometry) ([]byte, error) { return renderBody(rg) }

func renderBody(rg *RenderGeometry) ([]byte, error) {
	b, err := json.Marshal(rg)
	if err != nil {
		return nil, fmt.Errorf("track: сериализация геометрии: %w", err)
	}
	return b, nil
}

// writeDevice сериализует устройство для хеша: заголовок, порты, переходы.
// Вынесено отдельно, потому что число портов и переходов не фиксировано и
// правило записи должно быть в одном месте.
// Drive входит в выжимку по тому же доводу, что Kind у элемента: переводной
// механизм — факт О МОДЕЛИ, а не о её отрисовке. Стрелка, у которой ручной
// перевод заменили электроприводом, ведёт себя иначе (её переводят иначе), и
// клиент с горячим кэшем обязан узнать об этом сменой network_model_hash, а не
// от того, что у неё изменилась картинка.
func writeDevice(w io.Writer, d CompiledDevice) {
	fmt.Fprintf(w, "dev|%s|%s|%s|%s\n", d.ID, d.Hand, d.Drive, d.Resource)
	for _, p := range d.Ports {
		fmt.Fprintf(w, "dev.port|%s|%s\n", d.ID, p)
	}
	for _, tr := range d.Traversals {
		fmt.Fprintf(w, "dev.tr|%s|%s|%s|%s\n", d.ID, tr.Passage, tr.From, tr.To)
	}
}
