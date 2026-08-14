package worldstore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
)

// Замеры хранилища: сколько стоит положить и взять чанк, и сколько из этого
// приходится на кодек и хеш, а сколько на саму базу.

func benchStore(tb testing.TB) *Store {
	tb.Helper()
	s, err := Open(filepath.Join(tb.TempDir(), "world.db"))
	if err != nil {
		tb.Fatalf("открытие: %v", err)
	}
	tb.Cleanup(func() { s.Close() })
	// Правило подробности обязательно с тех пор, как охват приезжает картой:
	// регион без него хранилище отвергает. Замеры про это не знали и падали на
	// подготовке — то есть не снимались вовсе.
	if err := s.PutRegion(Region{ID: "ST_A", Frame: "{}", Epoch: 1, Rule: testRule}); err != nil {
		tb.Fatalf("регион: %v", err)
	}
	return s
}

// benchHeights — отсчёты, не сводимые к константе: сжатия в базе нет, но
// одинаковый блоб мог бы попасть в кэш страниц целиком.
func benchHeights(seed int) []int16 {
	h := make([]int16, chunk.Samples*chunk.Samples)
	for k := range h {
		h[k] = int16((k*37 + seed*11) % 4000)
	}
	return h
}

// BenchmarkOpenStore — цена Open: подключение и накатывание схемы.
func BenchmarkOpenStore(b *testing.B) {
	dir := b.TempDir()
	i := 0
	for b.Loop() {
		i++
		s, err := Open(filepath.Join(dir, fmt.Sprintf("w%d.db", i)))
		if err != nil {
			b.Fatal(err)
		}
		s.Close()
	}
}

// BenchmarkPutChunk — PutChunk целиком, каждый раз по НОВОМУ адресу:
// вставка, а не обновление.
func BenchmarkPutChunk(b *testing.B) {
	s := benchStore(b)
	h := benchHeights(1)
	i := 0
	for b.Loop() {
		i++
		err := s.PutChunk(Chunk{
			Address:      chunk.Address{Region: "ST_A", Level: 0, CX: i, CZ: 0},
			WorldVersion: testWorldVersion,
			Revision:     1,
			BaseZmm:      140_000,
			Heights:      h,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOverwriteChunk — тот же адрес: ветка ON CONFLICT DO UPDATE.
func BenchmarkOverwriteChunk(b *testing.B) {
	s := benchStore(b)
	h := benchHeights(2)
	for b.Loop() {
		err := s.PutChunk(Chunk{
			Address:      chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 0},
			WorldVersion: testWorldVersion,
			Revision:     1,
			BaseZmm:      140_000,
			Heights:      h,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCodecAndHash — то, что PutChunk делает ДО обращения к базе. Разница с
// записью и есть цена SQLite.
func BenchmarkCodecAndHash(b *testing.B) {
	h := benchHeights(3)
	b.Run("кодирование", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := chunk.EncodeHeights(h); err != nil {
				b.Fatal(err)
			}
		}
	})
	blob, err := chunk.EncodeHeights(h)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("sha256", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s := sha256.New()
			fmt.Fprintf(s, "v1|%d|", int64(140_000))
			s.Write(blob)
			_ = hex.EncodeToString(s.Sum(nil))
		}
	})
	b.Run("декодирование", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := chunk.DecodeHeights(blob); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkGetChunk — GetChunk: запрос, блоб, декодирование.
func BenchmarkGetChunk(b *testing.B) {
	s := benchStore(b)
	const N = 512
	for i := range N {
		err := s.PutChunk(Chunk{
			Address:      chunk.Address{Region: "ST_A", Level: 0, CX: i, CZ: 0},
			WorldVersion: testWorldVersion,
			Revision:     1,
			BaseZmm:      140_000,
			Heights:      benchHeights(i),
		})
		if err != nil {
			b.Fatal(err)
		}
	}
	b.Run("попадание", func(b *testing.B) {
		b.ReportAllocs()
		i := 0
		for b.Loop() {
			i++
			c, ok, err := s.GetChunk(chunk.Address{Region: "ST_A", Level: 0, CX: i % N, CZ: 0}, testWorldVersion)
			if err != nil || !ok {
				b.Fatalf("чтение: %v, ok=%v", err, ok)
			}
			_ = c
		}
	})
	b.Run("промах", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, ok, err := s.GetChunk(chunk.Address{Region: "ST_A", Level: 0, CX: 1 << 20, CZ: 0}, testWorldVersion)
			if err != nil || ok {
				b.Fatalf("чтение: %v, ok=%v", err, ok)
			}
		}
	})
	b.Run("регион", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, ok, err := s.GetRegion("ST_A"); err != nil || !ok {
				b.Fatalf("регион: %v, ok=%v", err, ok)
			}
		}
	})
}

// BenchmarkBatchedWrite — ДИАГНОСТИКА, а не предложение: та же вставка, но
// сотня чанков в одной транзакции. Нужна затем, чтобы отделить цену самих
// данных от цены отдельной транзакции на каждый чанк. Продуктивный код не
// меняется — число говорит лишь, сколько в нём лежит запаса.
func BenchmarkBatchedWrite(b *testing.B) {
	s := benchStore(b)
	h := benchHeights(4)
	blob, err := chunk.EncodeHeights(h)
	if err != nil {
		b.Fatal(err)
	}
	i := 0
	for _, batch := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("по%d", batch), func(b *testing.B) {
			for b.Loop() {
				tx, err := s.db.Begin()
				if err != nil {
					b.Fatal(err)
				}
				for range batch {
					i++
					sum := sha256.New()
					fmt.Fprintf(sum, "v1|%d|", int64(140_000))
					sum.Write(blob)
					_, err := tx.Exec(`
						INSERT INTO chunks (region, level, cx, cz, revision, hash, base_z_mm, heights)
						VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
						"ST_A", 0, i, 1, int64(1), hex.EncodeToString(sum.Sum(nil)), int64(140_000), blob)
					if err != nil {
						b.Fatal(err)
					}
				}
				if err := tx.Commit(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(batch), "чанков/оп")
		})
	}
}
