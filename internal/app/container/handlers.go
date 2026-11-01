package container

import "github.com/gin-gonic/gin"

// HandlersWrapper groups the HTTP controllers of the service (filled by bootstrap).
type HandlersWrapper struct{}

type Middlewares map[string]gin.HandlerFunc
