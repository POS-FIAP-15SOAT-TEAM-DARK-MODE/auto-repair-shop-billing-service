package logger

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/env"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type customLogger struct {
	*zap.Logger
}

type ctxKey struct{}

var (
	globalLog  *customLogger
	dsnPattern *regexp.Regexp
)

func init() {
	initGlobalLogger()
}

func initGlobalLogger() {
	globalLog = &customLogger{initLogger()}
	dsnPattern = regexp.MustCompile(`://([^:/\s]+):([^@/\s]+)@`)
}

func initLogger() *zap.Logger {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.TimeKey = "timestamp"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncoderConfig.MessageKey = "message"
	cfg.Level = getLogLevel(env.GetString("LOG_LEVEL", "INFO"))

	l, err := cfg.Build()
	if err != nil {
		panic(err)
	}

	return l
}

func getLogLevel(level string) zap.AtomicLevel {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return zap.NewAtomicLevelAt(zap.DebugLevel)
	case "PANIC":
		return zap.NewAtomicLevelAt(zap.PanicLevel)
	default:
		return zap.NewAtomicLevelAt(zap.InfoLevel)
	}
}

func Global() *customLogger {
	return globalLog
}

// Request attaches a per-request logger to ctx, tagging every log line with
// a request_id. If incomingID is non-empty (e.g. forwarded by an upstream
// API Gateway or a client-supplied X-Request-Id), it's reused as-is so log
// lines for the same request stay correlated across services; otherwise a
// new one is generated.
func Request(ctx context.Context, incomingID string) (context.Context, string) {
	reqId := incomingID
	if reqId == "" {
		reqId = uuid.New().String()
	}
	l := initLogger().With(
		zap.String("request_id", reqId),
	)
	return withContext(ctx, &customLogger{l}), reqId
}

func Of(ctx context.Context) *customLogger {
	if ctx == nil {
		return Global()
	}

	if l, ok := ctx.Value(ctxKey{}).(*customLogger); ok {
		return l
	}
	return Global()
}

func withContext(ctx context.Context, l *customLogger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

func (l *customLogger) Info(msg string, fields ...zap.Field) {
	l.Logger.Info(msg, sanitizeFields(fields)...)
}

func (l *customLogger) Warn(msg string, fields ...zap.Field) {
	l.Logger.Warn(msg, sanitizeFields(fields)...)
}

func (l *customLogger) Error(err error, fields ...zap.Field) {
	fields = append(fields, zap.Error(err))
	l.Logger.Error(err.Error(), sanitizeFields(fields)...)
}

func (l *customLogger) Fatal(msg string, fields ...zap.Field) {
	l.Logger.Fatal(msg, sanitizeFields(fields)...)
}

func (l *customLogger) Debug(msg string, fields ...zap.Field) {
	l.Logger.Debug(msg, sanitizeFields(fields)...)
}

func (l *customLogger) Sync() {
	if l != nil {
		if err := l.Logger.Sync(); err != nil {
			err = fmt.Errorf("failed to sync logger: %w", err)
			l.Error(err)
		}
	}
}

func sanitizeFields(fields []zap.Field) []zap.Field {
	sanitized := make([]zap.Field, 0, len(fields))
	for _, field := range fields {
		key := strings.ToLower(strings.TrimSpace(field.Key))
		if isSensitiveKey(key) {
			sanitized = append(sanitized, zap.String(field.Key, "[REDACTED]"))
			continue
		}
		if field.Type == zapcore.StringType {
			value := redactDSNPassword(field.String)
			sanitized = append(sanitized, zap.String(field.Key, value))
			continue
		}
		sanitized = append(sanitized, field)
	}
	return sanitized
}

func isSensitiveKey(key string) bool {
	return key == "password" ||
		key == "senha" ||
		key == "pwd" ||
		key == "token" ||
		key == "authorization" ||
		key == "cpf" ||
		key == "cnpj" ||
		key == "document" ||
		key == "documento"
}

func redactDSNPassword(value string) string {
	return dsnPattern.ReplaceAllString(value, `://$1:[REDACTED]@`)
}
