package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Server        ServerConfig
	Database      DatabaseConfig
	Auth          AuthConfig
	Log           LogConfig
	LLM           LLMConfig
	Agent         AgentConfig
	Search        SearchConfig
	Docker        DockerConfig
	Embedding     EmbeddingConfig
	Observability ObservabilityConfig
	Scraper       ScraperConfig
}

type ScraperConfig struct {
	PublicURL  string // Full base URL for public targets, may include credentials
	PrivateURL string // Full base URL for private/internal targets, may include credentials
}

type DockerConfig struct {
	Enabled          bool
	SocketPath       string
	DefaultImage     string
	Network          string
	DataDir          string
	CPULimit         float64
	MemoryLimitMB    int
	ContainerTimeout int // seconds
	Inside           bool // true when sentrix itself runs inside Docker
}

type EmbeddingConfig struct {
	Provider   string // "openai", "ollama", "none"
	Model      string
	APIKey     string
	BaseURL    string
	Dimensions int
	BatchSize  int
}

type ObservabilityConfig struct {
	Enabled          bool
	ServiceName      string
	OTLPEndpoint     string
	TraceSampleRate  float64
	LangfuseEnabled  bool
	LangfusePublicKey string
	LangfuseSecretKey string
	LangfuseHost     string
}

type AgentConfig struct {
	Workers               int
	SameToolLimit         int
	TotalToolLimit        int
	TaskToolLimit         int // max total tool calls across all subtasks in one task (0 = TotalToolLimit * 3)
	SqlmapTimeoutSeconds  int
}

type LLMConfig struct {
	OpenAIKey       string
	AnthropicKey    string
	GeminiKey       string
	DeepSeekKey     string
	OllamaURL       string
	CustomURL       string
	CustomModel     string
	CustomAPIKey    string
	DefaultProvider string
}

type SearchConfig struct {
	DefaultMaxResults int
	TimeoutSeconds    int
	ProviderPriority  []string

	DuckDuckGoEnabled bool

	GoogleAPIKey string
	GoogleCX     string

	TavilyAPIKey string

	TraversaalAPIKey string

	PerplexityAPIKey string
	PerplexityModel  string

	SearxngURL        string
	SearxngLanguage   string
	SearxngCategories string
	SearxngSafeSearch string

	SploitusEnabled bool

	BrowserUserAgent string
}

type ServerConfig struct {
	Host           string
	Port           int
	AllowedOrigins string
}

type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

type AuthConfig struct {
	JWTSecret   string
	TokenExpiry int
}

type LogConfig struct {
	Level  string
	Format string
}

func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Host:           env("SERVER_HOST", "0.0.0.0"),
			Port:           envInt("SERVER_PORT", 8080),
			AllowedOrigins: env("ALLOWED_ORIGINS", "*"),
		},
		Database: DatabaseConfig{
			Host:     env("DB_HOST", "localhost"),
			Port:     envInt("DB_PORT", 5432),
			User:     env("DB_USER", "sentrix"),
			Password: env("DB_PASSWORD", "sentrix"),
			Name:     env("DB_NAME", "sentrix"),
			SSLMode:  env("DB_SSLMODE", "disable"),
		},
		Auth: AuthConfig{
			JWTSecret:   env("JWT_SECRET", ""),
			TokenExpiry: envInt("JWT_EXPIRY_HOURS", 24),
		},
		Log: LogConfig{
			Level:  env("LOG_LEVEL", "info"),
			Format: env("LOG_FORMAT", "text"),
		},
		LLM: LLMConfig{
			OpenAIKey:       env("OPENAI_API_KEY", ""),
			AnthropicKey:    env("ANTHROPIC_API_KEY", ""),
			GeminiKey:       env("GEMINI_API_KEY", ""),
			DeepSeekKey:     env("DEEPSEEK_API_KEY", ""),
			OllamaURL:       env("OLLAMA_URL", ""),
			CustomURL:       env("CUSTOM_LLM_URL", ""),
			CustomModel:     env("CUSTOM_LLM_MODEL", ""),
			CustomAPIKey:    env("CUSTOM_LLM_API_KEY", ""),
			DefaultProvider: env("DEFAULT_LLM_PROVIDER", ""),
		},
		Agent: AgentConfig{
			Workers:              envInt("AGENT_WORKERS", 2),
			SameToolLimit:        envInt("AGENT_SAME_TOOL_LIMIT", 4),
			TotalToolLimit:       envInt("AGENT_TOTAL_TOOL_LIMIT", 20),
			TaskToolLimit:        envInt("AGENT_TASK_TOOL_LIMIT", 60),
			SqlmapTimeoutSeconds: envInt("SQLMAP_DEFAULT_TIMEOUT_SECONDS", 180),
		},
		Search: SearchConfig{
			DefaultMaxResults: envInt("SEARCH_DEFAULT_MAX_RESULTS", 5),
			TimeoutSeconds:    envInt("SEARCH_TIMEOUT_SECONDS", 30),
			ProviderPriority: envCSV(
				"SEARCH_PROVIDER_PRIORITY",
				[]string{"duckduckgo", "tavily", "searxng", "google", "perplexity", "traversaal", "sploitus"},
			),
			DuckDuckGoEnabled: envBool("DUCKDUCKGO_ENABLED", true),
			GoogleAPIKey:      env("GOOGLE_SEARCH_API_KEY", ""),
			GoogleCX:          env("GOOGLE_SEARCH_CX", ""),
			TavilyAPIKey:      env("TAVILY_API_KEY", ""),
			TraversaalAPIKey:  env("TRAVERSAAL_API_KEY", ""),
			PerplexityAPIKey:  env("PERPLEXITY_API_KEY", ""),
			PerplexityModel:   env("PERPLEXITY_MODEL", "sonar"),
			SearxngURL:        env("SEARXNG_URL", ""),
			SearxngLanguage:   env("SEARXNG_LANGUAGE", "en"),
			SearxngCategories: env("SEARXNG_CATEGORIES", "general"),
			SearxngSafeSearch: env("SEARXNG_SAFE_SEARCH", "1"),
			SploitusEnabled:   envBool("SPLOITUS_ENABLED", true),
			BrowserUserAgent:  env("BROWSER_USER_AGENT", "Sentrix/0.7"),
		},
		Docker: DockerConfig{
			Enabled:          envBool("DOCKER_ENABLED", false),
			SocketPath:       env("DOCKER_SOCKET", "/var/run/docker.sock"),
			DefaultImage:     env("DOCKER_DEFAULT_IMAGE", "sentrix-tools:latest"),
			Network:          env("DOCKER_NETWORK", "sentrix-sandbox"),
			DataDir:          env("DOCKER_DATA_DIR", "./data/sandbox"),
			CPULimit:         envFloat("DOCKER_CPU_LIMIT", 1.0),
			MemoryLimitMB:    envInt("DOCKER_MEMORY_LIMIT_MB", 512),
			ContainerTimeout: envInt("DOCKER_CONTAINER_TIMEOUT", 1800),
			Inside:           envBool("DOCKER_INSIDE", false),
		},
		Embedding: EmbeddingConfig{
			Provider:   env("EMBEDDING_PROVIDER", "none"),
			Model:      env("EMBEDDING_MODEL", "text-embedding-3-small"),
			APIKey:     env("EMBEDDING_API_KEY", ""),
			BaseURL:    env("EMBEDDING_BASE_URL", ""),
			Dimensions: envInt("EMBEDDING_DIMENSIONS", 1536),
			BatchSize:  envInt("EMBEDDING_BATCH_SIZE", 100),
		},
		Scraper: ScraperConfig{
			PublicURL:  env("SCRAPER_PUBLIC_URL", ""),
			PrivateURL: env("SCRAPER_PRIVATE_URL", ""),
		},
		Observability: ObservabilityConfig{
			Enabled:          envBool("OBSERVABILITY_ENABLED", false),
			ServiceName:      env("OTEL_SERVICE_NAME", "sentrix"),
			OTLPEndpoint:     env("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
			TraceSampleRate:  envFloat("OTEL_TRACE_SAMPLE_RATE", 1.0),
			LangfuseEnabled:  envBool("LANGFUSE_ENABLED", false),
			LangfusePublicKey: env("LANGFUSE_PUBLIC_KEY", ""),
			LangfuseSecretKey: env("LANGFUSE_SECRET_KEY", ""),
			LangfuseHost:     env("LANGFUSE_HOST", "https://cloud.langfuse.com"),
		},
	}

	// Fall back embedding API key to OpenAI key if not set.
	if cfg.Embedding.APIKey == "" {
		cfg.Embedding.APIKey = cfg.LLM.OpenAIKey
	}

	if cfg.Auth.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET environment variable is required")
	}

	return cfg, nil
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.Name, d.SSLMode,
	)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		switch v {
		case "1", "true", "TRUE", "True", "yes", "YES", "on", "ON":
			return true
		case "0", "false", "FALSE", "False", "no", "NO", "off", "OFF":
			return false
		}
	}
	return fallback
}

func envCSV(key string, fallback []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return append([]string{}, fallback...)
	}

	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return append([]string{}, fallback...)
	}
	return out
}
