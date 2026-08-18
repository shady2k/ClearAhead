package worldgen

import (
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

func newMap(t *testing.T) *mapfmt.Map {
	t.Helper()
	return seedmap.Station(seedmap.WithTerrain())
}

// ruleOf — правило подробности карты: ровно то, которое возьмёт Generate.
//
// Регион засевается ИМ ЖЕ, а не числами, написанными в тесте рядом: правило
// региона и правило карты обязаны совпадать (worldgen.sameRule), и тест,
// назвавший свои числа, проверял бы совпадение двух своих строк.
func ruleOf(tb testing.TB, m *mapfmt.Map) chunk.Rule {
	tb.Helper()
	r, err := terrain.RuleOf(m)
	if err != nil {
		tb.Fatal(err)
	}
	return r
}

func newStore(t *testing.T) *worldstore.Store {
	t.Helper()
	s, err := worldstore.Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	m := newMap(t)
	if err := s.PutRegion(worldstore.Region{ID: "ST_A", Frame: "{}", Epoch: 1, Rule: ruleOf(t, m), Domain: m.Terrain.Domain}); err != nil {
		t.Fatalf("регион: %v", err)
	}
	return s
}

func TestPipelineGeneratesChunks(t *testing.T) {
	s := newStore(t)
	rep, err := Generate(s, newMap(t), "ST_A", 1, 1)
	if err != nil {
		t.Fatalf("порождение: %v", err)
	}
	if rep.TotalChunks == 0 {
		t.Fatal("не порождено ни одного чанка")
	}
	t.Logf("чанков всего %d, байт %d (%.1f МБ)", rep.TotalChunks, rep.TotalBytes, float64(rep.TotalBytes)/1e6)
	for l := 0; l <= chunk.MaxLevelLimit; l++ {
		if rep.ByLevel[l] == 0 {
			continue
		}
		t.Logf("  уровень %d: %d", l, rep.ByLevel[l])
	}

	// Подробность обязана быть у пути: без чанков нулевого уровня земляные
	// работы просто не видны.
	if rep.ByLevel[0] == 0 {
		t.Fatal("ни одного чанка нулевого уровня")
	}
	// И обязана падать с удалением: если все чанки нулевые, правило не
	// работает и хранилище перестало быть разреженным.
	coarser := 0
	for l := 1; l <= chunk.MaxLevelLimit; l++ {
		coarser += rep.ByLevel[l]
	}
	if coarser == 0 {
		t.Fatal("ни одного чанка грубее нулевого уровня — правило подробности не сработало")
	}

	// Записанное читается.
	n, err := s.CountChunks("ST_A", 0)
	if err != nil {
		t.Fatalf("счёт: %v", err)
	}
	if n != rep.ByLevel[0] {
		t.Fatalf("в базе %d чанков нулевого уровня, отчёт говорит %d", n, rep.ByLevel[0])
	}
}

// Вдали от пути не хранится ничего: разреженность есть свойство хранилища.
func TestNoChunksFarFromTrack(t *testing.T) {
	s := newStore(t)
	if _, err := Generate(s, newMap(t), "ST_A", 1, 1); err != nil {
		t.Fatalf("порождение: %v", err)
	}
	// Сто километров от станции — заведомо за пределами последнего уровня.
	far := chunk.Address{Region: "ST_A", Level: 0, CX: 400000 / int(chunk.SideM(0)), CZ: 0}
	if _, ok, err := s.GetChunk(far, 1); err != nil {
		t.Fatalf("чтение: %v", err)
	} else if ok {
		t.Fatal("вдали от пути чанк всё-таки записан")
	}
}

// ИНВАРИАНТ ВХОДА: в базу попадает только то, что прошло полный путь. Карта, не
// проходящая валидацию, не должна оставить в базе ни одной записи.
func TestInvalidMapIsNotWritten(t *testing.T) {
	s := newStore(t)
	m := newMap(t)
	m.MapID = "СЛОМАННЫЙ:ID" // разделитель в идентификаторе запрещён

	if _, err := Generate(s, m, "ST_A", 1, 1); err == nil {
		t.Fatal("невалидная карта принята")
	}
	n, err := s.CountChunks("ST_A", 0)
	if err != nil {
		t.Fatalf("счёт: %v", err)
	}
	if n != 0 {
		t.Fatalf("после отказа в базе осталось %d чанков", n)
	}
}

// Регион должен существовать заранее: его геопривязку и происхождение конвейер
// придумать не вправе.
func TestWithoutRegionIsRefused(t *testing.T) {
	s, err := worldstore.Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	defer s.Close()
	if _, err := Generate(s, newMap(t), "ST_A", 1, 1); err == nil {
		t.Fatal("порождение прошло без заведённого региона")
	}
}

// Бутстрап заполняет пустую базу и НЕ трогает заполненную: перезапись
// существующего мира затравкой уничтожила бы правки редактора.
func TestBootstrapIsIdempotent(t *testing.T) {
	s, err := worldstore.Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	defer s.Close()

	m := seedmap.Station(seedmap.WithTerrain())

	rep, seeded, err := Bootstrap(s, m, 1, `{"source":"seedmap","kind":"fixture"}`)
	if err != nil {
		t.Fatalf("первый бутстрап: %v", err)
	}
	if !seeded {
		t.Fatal("первый бутстрап ничего не сделал на пустой базе")
	}
	// Бутстрап с 2026-08-13 чанков НЕ порождает: он заводит запись региона, а
	// прогрев — отдельный явный шаг (Generate), а не подразумеваемая часть
	// мира. Иначе «бутстрап не греет» ничем не отличалось бы от «греет».
	if rep.TotalChunks != 0 {
		t.Fatalf("бутстрап породил %d чанков — прогрев не должен подразумеваться", rep.TotalChunks)
	}
	// Прогрев включается явно — тем же шагом, каким его включает cmd/clearahead.
	if _, err := Generate(s, m, m.MapID, 1, 1); err != nil {
		t.Fatalf("прогрев: %v", err)
	}
	before, _ := s.CountChunks(m.MapID, 0)
	if before == 0 {
		t.Fatal("явный прогрев не породил ни одного чанка")
	}

	rep2, seeded2, err := Bootstrap(s, m, 2, `{"source":"seedmap","kind":"fixture"}`)
	if err != nil {
		t.Fatalf("второй бутстрап: %v", err)
	}
	if seeded2 {
		t.Fatal("второй бутстрап перезаписал заполненную базу")
	}
	after, _ := s.CountChunks(m.MapID, 0)
	if before != after {
		t.Fatalf("число чанков изменилось: было %d, стало %d", before, after)
	}
	// СВЕРКА СЕТИ НЕ СМЕЕТ ДВИГАТЬ ВЕРСИЮ НА СОВПАВШЕМ ТЕЛЕ. Иначе всякий
	// перезапуск публиковал бы новую версию мира, и клиент перезагружал бы
	// неизменившийся мир при каждом старте сервера — цена лечения u09k
	// оказалась бы выше самой болезни.
	if rep2.NetworkRepublished {
		t.Fatal("сеть пересобрана на той же карте: тело сборки недетерминировано или сверка сравнивает не то")
	}
	if rep2.WorldVersion != 1 {
		t.Fatalf("версия мира после второго бутстрапа %d, ожидалась 1", rep2.WorldVersion)
	}
}

// ТЕЛО СЕТИ В БАЗЕ, ПОСТРОЕННОЕ НЕ ЭТИМ КОДОМ, ОБЯЗАНО БЫТЬ ЗАМЕНЕНО НОВОЙ
// ВЕРСИЕЙ, а не отдано игре молча (бида ClearAhead-u09k).
//
// Смену КОДА геометрии тест подделать не может — он собран тем же кодом. Он
// воспроизводит РОВНО ТО СОСТОЯНИЕ, которым дефект проявлялся: в базе лежит
// тело, отличное от того, что строится сейчас. Откуда взялось расхождение —
// правка построителя или правка карты, — сверке безразлично, и в этом её смысл:
// она сравнивает байты, а не поводы.
func TestBootstrapRepublishesNetworkThatCurrentCodeDoesNotBuild(t *testing.T) {
	s, err := worldstore.Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	defer s.Close()

	// Две карты одного региона: рельеф, домен и правило подробности те же
	// (иначе бутстрап откажет раньше сверки — sameRule, sameDomain), а тело
	// сети разное — во второй перегон несом мостом, и сооружение едет в сеть.
	// Разойтись обязана ровно она.
	stale := seedmap.Line(seedmap.WithTerrain())
	fresh := seedmap.Line(seedmap.WithTerrain(),
		seedmap.WithCarryingStructure("bridge", "MOST", seedmap.LineEdgeID, 0, seedmap.LineLengthM))
	region := fresh.MapID

	if _, _, err := Bootstrap(s, stale, 1, `{"source":"seedmap","kind":"fixture"}`); err != nil {
		t.Fatalf("засев старым телом: %v", err)
	}
	oldBody, _, ok, err := s.GetNetwork(region, 1)
	if err != nil || !ok {
		t.Fatalf("сеть версии 1: ok=%v err=%v", ok, err)
	}

	rep, seeded, err := Bootstrap(s, fresh, 1, `{"source":"seedmap","kind":"fixture"}`)
	if err != nil {
		t.Fatalf("бутстрап на разошедшемся теле: %v", err)
	}
	if seeded {
		t.Fatal("бутстрап пересеял базу вместо публикации")
	}
	if !rep.NetworkRepublished {
		t.Fatal("расхождение тела не замечено: игра получила бы сеть, которой этот код не строит")
	}
	if rep.WorldVersion != 2 {
		t.Fatalf("опубликована версия %d, ожидалась 2", rep.WorldVersion)
	}

	head, ok, err := s.GetProjectionHead(region)
	if err != nil || !ok {
		t.Fatalf("голова проекций: ok=%v err=%v", ok, err)
	}
	if head.WorldVersion != 2 || head.NetworkVersion != 2 {
		t.Fatalf("голова назвала версию мира %d и версию сети %d, ожидались 2 и 2",
			head.WorldVersion, head.NetworkVersion)
	}

	_, want, err := seedHead(fresh)
	if err != nil {
		t.Fatalf("эталонное тело: %v", err)
	}
	got, _, ok, err := s.GetNetwork(region, head.WorldVersion)
	if err != nil || !ok {
		t.Fatalf("сеть версии %d: ok=%v err=%v", head.WorldVersion, ok, err)
	}
	if got != string(want) {
		t.Fatalf("под версией %d лежит не то тело, которое строит этот код (%d байт против %d)",
			head.WorldVersion, len(got), len(want))
	}

	// НЕИЗМЕНЯЕМОСТЬ ПРЕЖНЕГО АДРЕСА — половина довода, по которому публикуется
	// НОВАЯ версия, а не правится тело текущей: клиент, забравший версию 1 под
	// Cache-Control: immutable, обязан и дальше получать под ней ровно то, что
	// забрал.
	back, _, ok, err := s.GetNetwork(region, 1)
	if err != nil || !ok {
		t.Fatalf("сеть версии 1 после публикации: ok=%v err=%v", ok, err)
	}
	if back != oldBody {
		t.Fatal("тело версии 1 переписано публикацией: immutable на версионном адресе стал ложью")
	}
}
