# Butuh: Go 1.26+, Docker. Di Windows jalankan lewat Git Bash / WSL, atau lihat README untuk perintah manual.
.PHONY: up down migrate-up migrate-down run test vet fmt check

up:            ## Jalankan PostgreSQL di Docker lalu terapkan migrasi
	docker compose up -d db
	docker compose run --rm migrate up

down:          ## Hentikan PostgreSQL (data tetap tersimpan di volume)
	docker compose down

migrate-up:    ## Terapkan semua migrasi
	docker compose run --rm migrate up

migrate-down:  ## Batalkan 1 migrasi terakhir
	docker compose run --rm migrate down 1

run:           ## Jalankan API (membaca .env)
	go run ./cmd/api

test:          ## Semua test dengan race detector
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

check: fmt vet test  ## Cek lengkap sebelum commit
