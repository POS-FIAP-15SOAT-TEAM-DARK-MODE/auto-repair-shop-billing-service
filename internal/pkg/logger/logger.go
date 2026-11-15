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

const defaultServiceName = "billing-service"

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
	l, err := productionConfig(getLogLevel(env.GetString("LOG_LEVEL", "INFO"))).Build()
	if err != nil {
		panic(err)
	}

	return l
}

// productionConfig is the JSON encoder config shared by every log line. The
// `service` field is set once here so each line says which service emitted it.
func productionConfig(level zap.AtomicLevel) zap.Config {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.TimeKey = "timestamp"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncoderConfig.MessageKey = "message"
	cfg.InitialFields = map[string]any{"service": serviceName()}
	cfg.Level = level
	return cfg
}

// serviceName reads SERVICE_NAME (set per deployment) and falls back to the
// name of this service.
func serviceName() string {
	return env.GetString("SERVICE_NAME", defaultServiceName)
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

// ReplaceGlobal swaps the process-wide logger and returns a function that
// restores the previous one. It exists so tests can capture log output.
func ReplaceGlobal(l *zap.Logger) (restore func()) {
	prev := globalLog
	globalLog = &customLogger{l}
	return func() { globalLog = prev }
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
	return With(ctx, zap.String("request_id", reqId)), reqId
}

// With returns a context whose logger carries fields on every line. Consumers
// use it to add message_id, saga_id and service_order_id to the context.
func With(ctx context.Context, fields ...zap.Field) context.Context {
	return withContext(ctx, Of(ctx).With(fields...))
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

// With derives a logger that adds fields to every line, after redaction.
func (l *customLogger) With(fields ...zap.Field) *customLogger {
	return &customLogger{l.Logger.With(sanitizeFields(fields)...)}
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
		key == "jwt" ||
		key == "access_token" ||
		key == "authorization" ||
		key == "cpf" ||
		key == "cnpj" ||
		key == "document" ||
		key == "documento"
}

func redactDSNPassword(value string) string {
	return dsnPattern.ReplaceAllString(value, `://$1:[REDACTED]@`)
}
