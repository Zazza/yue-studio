package config

import (
	"os"
	"runtime"
)

type Config struct {
	YueURL string
}

func Load() *Config {
	def := "http://192.168.1.184:8091"
	if runtime.GOOS == "windows" {
		// воркер поднимается в WSL2 на этой же машине — localhost-форвардинг
		def = "http://localhost:8091"
	}
	return &Config{
		YueURL: env("YUE_URL", def),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
