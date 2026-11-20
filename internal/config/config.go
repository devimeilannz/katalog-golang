package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config berisi seluruh konfigurasi aplikasi.
type Config struct {
	Port         string
	DatabaseURL  string
	QueryTimeout time.Duration
	ShutdownWait time.Duration
	LogLevel     string
}

func Load() (*Config, error) {
	loadDotEnv(".env")

	cfg := &Config{
		Port:        getEnv("APP_PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL wajib diisi (lihat .env.example)")
	}

	qt, err := getSeconds("DB_QUERY_TIMEOUT_SECONDS", 5)
	if err != nil {
		return nil, err
	}
	sw, err := getSeconds("SHUTDOWN_TIMEOUT_SECONDS", 10)
	if err != nil {
		return nil, err
	}
	cfg.QueryTimeout, cfg.ShutdownWait = qt, sw
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getSeconds(key string, fallback int) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return time.Duration(fallback) * time.Second, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s harus berupa bilangan bulat >= 1, dapat %q", key, v)
	}
	return time.Duration(n) * time.Second, nil
}

// loadDotEnv membaca baris KEY=VALUE sederhana. File yang tidak ada diabaikan.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
	if err := sc.Err(); err != nil {
		return
	}
}
