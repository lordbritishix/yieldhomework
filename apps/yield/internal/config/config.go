package config

import (
	"github.com/joho/godotenv"
	"log"
	"os"
	"strconv"
)

type Config struct {
	RpcURL         string
	DbURL          string
	KafkaBroker    string
	KafkaTopic     string
	ChunkSize      uint64
	FinalityOffset uint64
	APIPort        int
}

// NewConfig loads configuration from environment variables
func NewConfig() *Config {
	// Load .env file (ignore error if file doesn't exist)
	if err := godotenv.Load(); err != nil {
		log.Fatalf("Warning: Could not load .env file: %v", err)
	}

	return &Config{
		RpcURL:         getEnvOrFatal("RPC_URL"),
		DbURL:          getEnvOrFatal("DB_URL"),
		KafkaBroker:    getEnvOrFatal("KAFKA_BROKER"),
		KafkaTopic:     getEnvOrFatal("KAFKA_TOPIC"),
		ChunkSize:      getEnvUint64("CHUNK_SIZE", 100),
		FinalityOffset: getEnvUint64("FINALITY_OFFSET", 12),
		APIPort:        getEnvInt("API_PORT", 8080),
	}
}

func getEnvOrFatal(key string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	log.Fatalf("Warning: environment variable %s not set", key)

	return ""
}

func getEnvUint64(key string, defaultValue uint64) uint64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}
