package logging

import (
	"context"
	"log/slog"
	"os"

	"github.com/aura/aura/internal/health"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// zapHandler implements slog.Handler by delegating to zap.
type zapHandler struct {
	core   zapcore.Core
	logger *zap.Logger
	group  string
	attrs  []slog.Attr
}

// Setup initializes zap as the structured logger with secret sanitization.
func Setup(level string) *slog.Logger {
	zapLevel := zapLevel(level)
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "ts"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.AddSync(os.Stdout),
		zapLevel,
	)
	zapLogger := zap.New(core, zap.AddCaller())

	handler := &zapHandler{core: core, logger: zapLogger}
	// Wrap with sanitize handler to redact secrets
	sanitized := health.NewSanitizeHandler(handler)

	logger := slog.New(sanitized)
	slog.SetDefault(logger)
	return logger
}

func (h *zapHandler) Enabled(ctx context.Context, level slog.Level) bool {
	var zapLevel zapcore.Level
	switch {
	case level >= slog.LevelError:
		zapLevel = zapcore.ErrorLevel
	case level >= slog.LevelWarn:
		zapLevel = zapcore.WarnLevel
	case level >= slog.LevelInfo:
		zapLevel = zapcore.InfoLevel
	default:
		zapLevel = zapcore.DebugLevel
	}
	return h.core.Enabled(zapLevel)
}

func (h *zapHandler) Handle(ctx context.Context, r slog.Record) error {
	fields := attrsToZapFields(r.NumAttrs(), func(visit func(slog.Attr) bool) {
		r.Attrs(visit)
	})

	switch {
	case r.Level >= slog.LevelError:
		h.logger.Error(r.Message, fields...)
	case r.Level >= slog.LevelWarn:
		h.logger.Warn(r.Message, fields...)
	case r.Level >= slog.LevelInfo:
		h.logger.Info(r.Message, fields...)
	default:
		h.logger.Debug(r.Message, fields...)
	}
	return nil
}

func (h *zapHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &zapHandler{
		core:   h.core,
		logger: h.logger,
		group:  h.group,
		attrs:  append(h.attrs, attrs...),
	}
}

func (h *zapHandler) WithGroup(name string) slog.Handler {
	return &zapHandler{
		core:   h.core,
		logger: h.logger,
		group:  name,
		attrs:  h.attrs,
	}
}

func attrToZapField(a slog.Attr) zap.Field {
	val := a.Value.Resolve()
	switch val.Kind() {
	case slog.KindString:
		return zap.String(a.Key, val.String())
	case slog.KindInt64:
		return zap.Int64(a.Key, val.Int64())
	case slog.KindFloat64:
		return zap.Float64(a.Key, val.Float64())
	case slog.KindBool:
		return zap.Bool(a.Key, val.Bool())
	case slog.KindDuration:
		return zap.Duration(a.Key, val.Duration())
	case slog.KindTime:
		return zap.Time(a.Key, val.Time())
	default:
		return zap.String(a.Key, val.String())
	}
}

func attrsToZapFields(hint int, visit func(func(slog.Attr) bool)) []zap.Field {
	fields := make([]zap.Field, 0, hint)
	visit(func(a slog.Attr) bool {
		fields = append(fields, attrToZapField(a))
		return true
	})
	return fields
}

func zapLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// NewNopLogger returns a logger that discards all output (for tests).
func NewNopLogger() *slog.Logger {
	return slog.New(discardHandler{})
}

type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool    { return false }
func (d discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler       { return d }
func (d discardHandler) WithGroup(string) slog.Handler             { return d }