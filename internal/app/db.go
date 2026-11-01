package app

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/env"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

var (
	instance  *sql.DB
	once      sync.Once
	sqlOpenFn = sql.Open
)

func DBConnect() *sql.DB {
	once.Do(func() {
		connStr := fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=%s",
			env.GetString("POSTGRES_USER", "postgres"),
			env.GetString("POSTGRES_PASSWORD", "postgres"),
			env.GetString("POSTGRES_HOST", "localhost"),
			env.GetString("POSTGRES_PORT", "5432"),
			env.GetString("POSTGRES_DB", "autorepairshop"),
			env.GetString("POSTGRES_SSLMODE", "disable"),
		)

		logger.Global().Debug("Database conn str", zap.String("value", connStr))
		conn, err := sqlOpenFn("postgres", connStr)
		if err != nil {
			if conn != nil {
				_ = conn.Close()
			}
			logger.Global().Fatal("failed to connect to database: ", zap.Error(err))
			return
		}

		if pingErr := conn.Ping(); pingErr != nil {
			_ = conn.Close()
			logger.Global().Fatal("failed to ping database: ", zap.Error(pingErr))
		}

		instance = conn
	})

	return instance
}

func ConnectWithDB(db *sql.DB) {
	once.Do(func() {})
	instance = db
}
