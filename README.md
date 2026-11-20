# Katalog Produk API (Go + PostgreSQL)

REST API katalog produk dengan kategori. Dibuat dengan Go 1.26.0, PostgreSQL 15+, `net/http`, dan `database/sql` (tanpa ORM, query SQL terlihat jelas).

Endpoint yang diimplementasikan: **CRUD kategori** (`/api/v1/categories`) dan **health check** (`/health`).

## 1. Cara menjalankan dari nol

Prasyarat: Go 1.26+, Docker (untuk PostgreSQL), Git.

```bash
git clone <url-repo-kamu> katalog-api && cd katalog-api
cp .env.example .env        # Windows PowerShell: copy .env.example .env
make up                     # PostgreSQL 15 di Docker + terapkan migrasi
make run                    # API berjalan di http://localhost:8080
```

Tanpa `make` (misalnya Windows):

```bash
docker compose up -d db
docker compose run --rm migrate up
go run ./cmd/api
```

Tanpa Docker: install PostgreSQL 15+, buat user dan database sesuai `DATABASE_URL` di `.env`, lalu terapkan file di `migrations/` secara berurutan (lihat bagian 2).

Cek: `curl http://localhost:8080/health`

## 2. Migrasi dan test

Migrasi memakai format [golang-migrate](https://github.com/golang-migrate/migrate): setiap versi punya file `.up.sql` dan `.down.sql` di folder `migrations/`.

```bash
make migrate-up     # terapkan semua migrasi
make migrate-down   # batalkan 1 migrasi terakhir
```

Alternatif dengan CLI `migrate` yang sudah ter-install:

```bash
migrate -path migrations -database "$DATABASE_URL" up
migrate -path migrations -database "$DATABASE_URL" down 1
```

Test dan pengecekan kode:

```bash
make test           # go test -race ./...
make vet            # go vet ./...
make fmt            # gofmt -w .
make check          # fmt + vet + test
```

Unit test layer service memakai repository tiruan buatan sendiri (`fakeRepo`) dan gaya table-driven. Ada juga test handler dengan `httptest` untuk memastikan envelope dan pemetaan status.

## 3. Contoh request (curl)

Semua response memakai envelope `success`, `message`, `data`/`error`.

```bash
BASE=http://localhost:8080/api/v1

# Buat kategori (201)
curl -X POST $BASE/categories -H "Content-Type: application/json" \
  -d '{"name":"Elektronik","description":"Perangkat elektronik dan aksesori"}'

# Daftar kategori dengan pagination dan pencarian (200)
curl "$BASE/categories?page=1&limit=10&search=elek"

# Detail kategori + product_count (200)
curl $BASE/categories/<ID>

# Ubah kategori, penggantian penuh: semua field wajib dikirim (200)
curl -X PUT $BASE/categories/<ID> -H "Content-Type: application/json" \
  -d '{"name":"Elektronik Rumah","description":"Peralatan elektronik rumah tangga"}'

# Soft delete (200, atau 409 jika masih ada produk aktif)
curl -X DELETE $BASE/categories/<ID>

# Contoh error
curl -X POST $BASE/categories -H "Content-Type: application/json" -d '{"name":"a"}'   # 400 VALIDATION_ERROR
curl "$BASE/categories?limit=500"                                                       # 400 VALIDATION_ERROR
curl $BASE/categories/abc                                                               # 400 (ID bukan UUID)
```

Contoh error validasi (semua pelanggaran dikembalikan sekaligus):

```json
{
  "success": false,
  "message": "Validasi gagal",
  "error": {
    "code": "VALIDATION_ERROR",
    "details": [
      { "field": "name", "message": "name harus 3-100 karakter (saat ini 1)" },
      { "field": "description", "message": "description maksimal 500 karakter (saat ini 501)" }
    ]
  }
}
```

## 4. Asumsi dan keputusan desain

- **Tiga layer.** `handler` (parsing request, validasi format, mapping error ke HTTP), `service` (aturan bisnis, transaksi), `repository` (SQL). Handler bergantung pada interface service, service pada interface repository, semua disuntikkan lewat constructor tanpa variabel global. Alasannya: tiap layer bisa dites sendiri dengan fake.
- **Interface didefinisikan di sisi pemakai** (`handler.CategoryService`, `service.CategoryRepository`), sesuai idiom Go, sehingga mudah dibuat fake.
- **Error terdefinisi di service** (`ErrValidation`, `ErrNotFound`, `ErrConflict`) dan dipetakan ke status + kode pada **satu fungsi** `writeError` di `internal/handler/response.go`. Error tak terduga menjadi `500 INTERNAL_ERROR` dengan pesan generik; detail teknis hanya ke log.
- **Soft delete** memakai kolom `deleted_at`. Semua query memfilter `deleted_at IS NULL`, jadi data terhapus tidak muncul di endpoint mana pun.
- **Nama kategori unik di antara yang belum dihapus** lewat *partial unique index* (`WHERE deleted_at IS NULL`). Dengan begitu nama bisa dipakai lagi setelah kategori lama dihapus, dan pengecekan duplikat bebas dari race condition (tidak perlu SELECT lalu INSERT). Perbandingan nama *case-sensitive*.
- **DELETE memakai transaksi** dengan `SELECT ... FOR UPDATE` pada baris kategori, lalu hitung produk aktif, lalu soft delete. Jika masih ada produk aktif: 409 `Kategori masih digunakan oleh N produk aktif`.
- **PUT = penggantian penuh**: `name` dan `description` wajib dikirim. `description` tidak dikirim atau `null` pada PUT menghasilkan 400.
- **Validasi** menghitung karakter (rune), bukan byte, setelah spasi di ujung nama dibuang (`TrimSpace`). Aturan: nama 3-100 karakter, deskripsi maksimal 500 karakter. Database juga punya `CHECK` sebagai pengaman kedua.
- **Context dan timeout**: `context` request diteruskan dari handler sampai query. Setiap query dibatasi `DB_QUERY_TIMEOUT_SECONDS` (default 5 detik).
- **Graceful shutdown** saat `SIGINT`/`SIGTERM`: server berhenti menerima request baru dan menunggu request berjalan (maksimal `SHUTDOWN_TIMEOUT_SECONDS`).
- **Route atau method yang tidak dikenal** mengembalikan `404 NOT_FOUND` dengan envelope standar (spesifikasi hanya mendefinisikan 200/201/400/404/409/500/503, jadi 405 tidak dipakai).
- **Tidak ada tabel `products`.** Project ini hanya punya tabel `categories`. `product_count` selalu `0` dan aturan hapus kategori dengan produk aktif (409) sudah tersedia di service, tetapi belum aktif karena repository mengembalikan 0 produk (lihat komentar di `CountActiveProducts`).
- **Satu dependency eksternal saja**: driver `github.com/lib/pq`. Router memakai `net/http` bawaan (pattern `{id}`), config dan `.env` dibaca dengan kode sendiri.
- **Format waktu** response: UTC, contoh `2026-11-17T06:30:00Z`.

## 5. Belum selesai / bisa diperbaiki jika ada waktu lebih

- Tabel dan endpoint produk belum ada. Jika dibutuhkan, buat migrasi `products` lalu aktifkan query di `CountActiveProducts` dan `GetByID`.
- Test integrasi repository dengan database asli (misalnya testcontainers) belum ada. Saat ini repository dites manual terhadap PostgreSQL.
- `Dockerfile` untuk membungkus API itu sendiri, dan job CI (`go vet`, `gofmt`, `go test -race`) di GitHub Actions.
- Request ID di log dan response agar error mudah dilacak.
- Opsi pencarian nama yang *case-insensitive* untuk keunikan nama kategori, serta pengurutan (`sort`) pada daftar kategori.
- Rate limiting dan autentikasi.

## Struktur proyek

```
.
├── cmd/api/main.go            # wiring dependency, start server, graceful shutdown
├── internal/
│   ├── config/                # baca environment / .env
│   ├── handler/               # parsing request, validasi format, mapping error ke HTTP
│   ├── service/               # aturan bisnis, transaksi
│   ├── repository/            # query SQL ke PostgreSQL
│   └── model/                 # struct domain dan DTO
├── migrations/                # file migrasi up dan down
├── docker-compose.yml
├── Makefile
├── .env.example
└── README.md
```
