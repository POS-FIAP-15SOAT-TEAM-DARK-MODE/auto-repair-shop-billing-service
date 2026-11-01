package container

import (
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/billing"
	"github.com/gin-gonic/gin"
)

// HandlersWrapper groups the HTTP controllers of the service.
type HandlersWrapper struct {
	Billing *billing.Controller
}

type Middlewares map[string]gin.HandlerFunc
