package main

import (
	"encoding/json"
	"os"
)

type Config struct {
	Quotes         []string `json:"quotes"`
	ChannelID      string   `json:"channel_id"`
	GemChannelID   string   `json:"gem_channel_id"`
	GemSubscribers []string `json:"gem_subscribers"`
}

var (
	config     Config
	configFile = "config.json"
)

func loadConfig() {
	data, err := os.ReadFile(configFile)
	if err != nil {
		config = Config{
			Quotes: []string{
				"Wytrwałość to klucz do sukcesu.",
				"Każdy dzień to nowa szansa.",
				"Wierz w siebie i swoje możliwości.",
			},
			ChannelID:      "",
			GemChannelID:   "",
			GemSubscribers: nil,
		}
		saveConfig()
		return
	}
	json.Unmarshal(data, &config)
}

func saveConfig() {
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(configFile, data, 0o644)
}
