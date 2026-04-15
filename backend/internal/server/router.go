package server

import (
	"context"
	"net/http"

	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/yourorg/sentrix/internal/agent"
	"github.com/yourorg/sentrix/internal/auth"
	"github.com/yourorg/sentrix/internal/graph"
	resthandler "github.com/yourorg/sentrix/internal/handler"
)

func (s *Server) registerRoutes() {
	authSvc := auth.NewService(s.cfg.Auth.JWTSecret, s.cfg.Auth.TokenExpiry)
	s.authSvc = authSvc

	// Health endpoint (no auth)
	s.router.GET("/health", s.healthCheck)

	// Serve frontend static files
	s.router.StaticFS("/assets", http.Dir("static/assets"))
	s.router.NoRoute(func(c *gin.Context) {
		c.File("static/index.html")
	})

	// ── Public API routes ──────────────────────────────────────────
	api := s.router.Group("/api/v1")
	{
		api.GET("/status", s.apiStatus)

		authH := resthandler.NewAuthHandler(s.db, authSvc)
		authGroup := api.Group("/auth")
		{
			authGroup.POST("/register", authH.Register)
			authGroup.POST("/login", authH.Login)
			authGroup.POST("/logout", authH.Logout)
		}
	}

	// ── Protected API routes ───────────────────────────────────────
	secured := s.router.Group("/api/v1")
	secured.Use(auth.RequireAuth(authSvc, s.db))
	{
		authH := resthandler.NewAuthHandler(s.db, authSvc)
		secured.GET("/auth/me", authH.Me)

		flowH := resthandler.NewFlowHandler(s.db, s.queue, s.broadcaster, s.cfg.Docker.DataDir, s.cfg.Scraper)
		assistantSvc := agent.NewAssistantService(
			s.db,
			s.registry,
			s.cfg,
			s.sandbox,
			s.memStore,
			s.telemetry.Tracer("sentrix.assistant"),
		)
		assistantH := resthandler.NewAssistantHandler(s.db, assistantSvc)
		flows := secured.Group("/flows")
		{
			flows.GET("", flowH.List)
			flows.POST("", flowH.Create)
			flows.GET("/:id", flowH.Get)
			flows.PUT("/:id", flowH.Update)
			flows.DELETE("/:id", flowH.Delete)
			flows.POST("/:id/start", flowH.Start)
			flows.POST("/:id/stop", flowH.Stop)
			flows.GET("/:id/events", flowH.Events)
			flows.GET("/:id/artifacts/:artifact_id/file", flowH.ArtifactFile)
			flows.GET("/:id/assistant", assistantH.Get)
			flows.PUT("/:id/assistant", assistantH.Update)
			flows.POST("/:id/assistant/messages", assistantH.SendMessage)
		}

		tokenH := resthandler.NewAPITokenHandler(s.db)
		tokens := secured.Group("/api-tokens")
		{
			tokens.GET("", tokenH.List)
			tokens.POST("", tokenH.Create)
			tokens.DELETE("/:id", tokenH.Delete)
		}

		providerH := resthandler.NewProviderHandler(s.db, s.registry)
		providers := secured.Group("/providers")
		{
			providers.GET("", providerH.List)
			providers.POST("", providerH.Create)
			providers.PUT("/:id", providerH.Update)
			providers.DELETE("/:id", providerH.Delete)
			providers.POST("/test", providerH.TestConnection)
			providers.GET("/available", providerH.Available)
		}

		if s.memStore != nil {
			memH := resthandler.NewMemoryHandler(s.memStore)
			memories := secured.Group("/memories")
			{
				memories.GET("", memH.List)
				memories.POST("/search", memH.Search)
				memories.DELETE("/:id", memH.Delete)
				memories.GET("/stats", memH.Stats)
			}
		}
	}

	// ── GraphQL ────────────────────────────────────────────────────
	gqlServer := handler.New(graph.NewExecutableSchema(graph.Config{
		Resolvers: &graph.Resolver{
			DB:          s.db,
			AuthSvc:     authSvc,
			Broadcaster: s.broadcaster,
			Scraper:     s.cfg.Scraper,
		},
	}))
	gqlServer.AddTransport(transport.Options{})
	gqlServer.AddTransport(transport.POST{})
	gqlServer.AddTransport(transport.Websocket{
		KeepAlivePingInterval: 10 * time.Second,
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	})
	gqlServer.Use(extension.Introspection{})

	// GraphQL endpoint (requires auth for POST, WS auth handled by transport)
	gqlHandler := func(c *gin.Context) {
		c.Request = c.Request.WithContext(
			context.WithValue(c.Request.Context(), "GinContextKey", c),
		)
		gqlServer.ServeHTTP(c.Writer, c.Request)
	}
	s.router.POST("/graphql", auth.RequireAuth(authSvc, s.db), gqlHandler)
	s.router.GET("/graphql", gqlHandler) // WebSocket upgrade endpoint

	// GraphQL Playground (dev only)
	if s.cfg.Log.Level == "debug" {
		s.router.GET("/playground", func(c *gin.Context) {
			playground.Handler("Sentrix GraphQL", "/graphql").ServeHTTP(c.Writer, c.Request)
		})
	}
}

func (s *Server) healthCheck(c *gin.Context) {
	sqlDB, err := s.db.DB()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  "database connection unavailable",
		})
		return
	}

	if err := sqlDB.Ping(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  "database ping failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}

func (s *Server) apiStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"name":    "sentrix",
		"version": "0.4.0",
	})
}
