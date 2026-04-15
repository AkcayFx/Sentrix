package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/agent"
	"github.com/yourorg/sentrix/internal/auth"
	"github.com/yourorg/sentrix/internal/config"
	"github.com/yourorg/sentrix/internal/embedding"
	"github.com/yourorg/sentrix/internal/memory"
	"github.com/yourorg/sentrix/internal/observability"
	"github.com/yourorg/sentrix/internal/provider"
	"github.com/yourorg/sentrix/internal/sandbox"
)

// Server encapsulates the HTTP server, router, and shared dependencies.
type Server struct {
	cfg         *config.Config
	db          *gorm.DB
	router      *gin.Engine
	http        *http.Server
	authSvc     *auth.Service
	registry    *provider.Registry
	broadcaster *agent.Broadcaster
	queue       *agent.Queue
	sandbox     sandbox.Client
	memStore    *memory.MemoryStore
	telemetry   *observability.Runtime
}

// New creates and configures a server instance with all middleware and routes.
func New(cfg *config.Config, db *gorm.DB) *Server {
	if cfg.Log.Level != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())

	telemetry, obsErr := observability.Init(context.Background(), cfg.Observability)
	if obsErr != nil {
		log.Warnf("observability: disabled - %v", obsErr)
	}

	router.Use(observability.GinMiddleware(telemetry))
	router.Use(requestLogger())
	router.Use(corsMiddleware(cfg.Server.AllowedOrigins))

	// Build the LLM provider registry from env-based configuration.
	reg := provider.NewRegistry(&provider.EnvLLMConfig{
		OpenAIKey:       cfg.LLM.OpenAIKey,
		AnthropicKey:    cfg.LLM.AnthropicKey,
		GeminiKey:       cfg.LLM.GeminiKey,
		DeepSeekKey:     cfg.LLM.DeepSeekKey,
		OllamaURL:       cfg.LLM.OllamaURL,
		CustomURL:       cfg.LLM.CustomURL,
		CustomModel:     cfg.LLM.CustomModel,
		CustomAPIKey:    cfg.LLM.CustomAPIKey,
		DefaultProvider: cfg.LLM.DefaultProvider,
	})

	// Initialize sandbox if Docker is enabled.
	var sb sandbox.Client
	if cfg.Docker.Enabled {
		var sbErr error
		sb, sbErr = sandbox.NewDockerClient(cfg.Docker)
		if sbErr != nil {
			log.Warnf("sandbox: disabled — %v", sbErr)
		} else {
			// Clean up orphaned containers from previous runs.
			if err := sb.CleanupOrphans(context.Background()); err != nil {
				log.Warnf("sandbox: orphan cleanup failed: %v", err)
			}
		}
	}

	// Initialize embedding provider.
	embedder, embErr := embedding.NewEmbedder(cfg.Embedding)
	if embErr != nil {
		log.Warnf("embedding: initialization failed — %v", embErr)
	}
	var memStore *memory.MemoryStore
	if embedder != nil {
		memStore = memory.NewMemoryStore(db, embedder)
		if memStore.Enabled() {
			log.Info("memory: vector memory system enabled")
		} else {
			log.Info("memory: store created but embedder unavailable")
		}
	}

	// Build the agent orchestration layer.
	broadcaster := agent.NewBroadcaster()
	orchestrator := agent.NewOrchestrator(db, reg, broadcaster, cfg, agent.OrchestratorConfig{
		SameToolLimit:  cfg.Agent.SameToolLimit,
		TotalToolLimit: cfg.Agent.TotalToolLimit,
	}, sb, memStore, telemetry.Tracer("sentrix.agent"))
	queue := agent.NewQueue(orchestrator, cfg.Agent.Workers)

	srv := &Server{
		cfg:         cfg,
		db:          db,
		router:      router,
		registry:    reg,
		broadcaster: broadcaster,
		queue:       queue,
		sandbox:     sb,
		memStore:    memStore,
		telemetry:   telemetry,
	}

	srv.registerRoutes()

	return srv
}

// Start begins listening on the configured address.
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)
	s.http = &http.Server{
		Addr:        addr,
		Handler:     s.router,
		// Use ReadHeaderTimeout instead of ReadTimeout so that long-lived
		// streams (SSE events, GraphQL subscriptions) are not terminated
		// after the header-read window expires.
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:  60 * time.Second,
	}

	log.Infof("server listening on %s", addr)
	return s.http.ListenAndServe()
}

// Shutdown performs a graceful shutdown.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.queue != nil {
		s.queue.Shutdown()
	}
	if s.sandbox != nil {
		s.sandbox.Close()
	}
	if s.telemetry != nil {
		if err := s.telemetry.Shutdown(ctx); err != nil {
			log.Warnf("observability: shutdown failed: %v", err)
		}
	}
	return s.http.Shutdown(ctx)
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.WithFields(log.Fields{
			"method":   c.Request.Method,
			"path":     c.Request.URL.Path,
			"status":   c.Writer.Status(),
			"duration": time.Since(start).String(),
			"ip":       c.ClientIP(),
		}).Info("request")
	}
}

func corsMiddleware(allowedOrigins string) gin.HandlerFunc {
	if allowedOrigins == "" {
		allowedOrigins = "*"
	}
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", allowedOrigins)
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
