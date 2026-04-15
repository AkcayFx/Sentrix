package graph

import (
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/agent"
	"github.com/yourorg/sentrix/internal/auth"
	"github.com/yourorg/sentrix/internal/config"
)

// Resolver is the root resolver for all GraphQL operations.
// It holds references to the database, auth service, and event broadcaster
// needed by query/mutation/subscription resolvers.
type Resolver struct {
	DB          *gorm.DB
	AuthSvc     *auth.Service
	Broadcaster *agent.Broadcaster
	Scraper     config.ScraperConfig
}
