package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

type Config struct {
	YueURL string
}

func Load() *Config {
	return &Config{
		YueURL: env("YUE_URL", loadSettings().ServerURL),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if def == "" {
		return "http://localhost:8091"
	}
	return def
}

// Settings — сохраняемые настройки приложения (settings.json в UserConfigDir).
type Settings struct {
	ServerURL string `json:"server_url"`
}

func settingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "yue-studio", "settings.json")
}

func loadSettings() Settings {
	var s Settings
	p := settingsPath()
	if p == "" {
		return s
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return s
	}
	json.Unmarshal(b, &s)
	return s
}

func SaveSettings(s Settings) {
	p := settingsPath()
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		log.Printf("settings: mkdir: %v", err)
		return
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		log.Printf("settings: save: %v", err)
	}
}
