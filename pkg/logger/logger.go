package logger

import (
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger defines the interface for logging operations
type Logger interface {
	Info(msg string, fields ...zap.Field)
	Error(msg string, fields ...zap.Field)
	Debug(msg string, fields ...zap.Field)
	Warn(msg string, fields ...zap.Field)
	Sync() error
}

// LoggingConfig represents logging configuration
// File sizes are in megabytes for rotation settings
type LoggingConfig struct {
	LogDir      string
	LogLevel    string // debug, info, warn, error
	MaxFileSize int    // in MB
	MaxBackups  int
	MaxAge      int // in days
}

// zapLogger implements the Logger interface using Uber Zap
type zapLogger struct {
	logger *zap.Logger
}

// NewLogger initializes a new stdout/stderr logger based on the provided log level.
// Kept for backward compatibility; prefer NewFileLogger for the application.
func NewLogger(level string) (Logger, error) {
	var cfg zap.Config

	switch level {
	case "debug":
		cfg = zap.NewDevelopmentConfig()
	case "info":
		cfg = zap.NewProductionConfig()
		cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	case "warn":
		cfg = zap.NewProductionConfig()
		cfg.Level = zap.NewAtomicLevelAt(zapcore.WarnLevel)
	case "error":
		cfg = zap.NewProductionConfig()
		cfg.Level = zap.NewAtomicLevelAt(zapcore.ErrorLevel)
	default:
		cfg = zap.NewProductionConfig()
		cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}

	logger, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	return &zapLogger{logger: logger}, nil
}

// NewFileLogger configures a zap logger to write to rotating log files only (no stderr)
func NewFileLogger(c LoggingConfig) (Logger, error) {
	if c.LogDir == "" {
		return &zapLogger{logger: zap.NewNop()}, nil
	}

	// Ensure log directory exists
	if err := os.MkdirAll(c.LogDir, 0755); err != nil {
		// Fall back to a no-op logger if we cannot create directory
		return &zapLogger{logger: zap.NewNop()}, nil
	}

	appPath := filepath.Join(c.LogDir, "app.log")
	errPath := filepath.Join(c.LogDir, "error.log")

	// Ensure log files exist so tests and tools can rely on their presence even if empty
	if f, err := os.OpenFile(appPath, os.O_CREATE|os.O_APPEND, 0644); err == nil {
		_ = f.Close()
	}
	if f, err := os.OpenFile(errPath, os.O_CREATE|os.O_APPEND, 0644); err == nil {
		_ = f.Close()
	}

	appWriter := zapcore.AddSync(&lumberjack.Logger{
		Filename:   appPath,
		MaxSize:    maxInt(c.MaxFileSize, 25), // default 25 MB
		MaxBackups: maxInt(c.MaxBackups, 3),
		MaxAge:     maxInt(c.MaxAge, 14), // days
		Compress:   false,
	})
	errWriter := zapcore.AddSync(&lumberjack.Logger{
		Filename:   errPath,
		MaxSize:    maxInt(c.MaxFileSize, 25),
		MaxBackups: maxInt(c.MaxBackups, 3),
		MaxAge:     maxInt(c.MaxAge, 14),
		Compress:   false,
	})

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.EncodeTime = func(t time.Time, pae zapcore.PrimitiveArrayEncoder) {
		pae.AppendString(t.Format(time.RFC3339))
	}
	encoder := zapcore.NewJSONEncoder(encCfg)

	// Level handling
	var minLevel zapcore.Level
	switch c.LogLevel {
	case "debug":
		minLevel = zapcore.DebugLevel
	case "warn":
		minLevel = zapcore.WarnLevel
	case "error":
		minLevel = zapcore.ErrorLevel
	default:
		minLevel = zapcore.InfoLevel
	}

	// Core for app.log: Debug..Warn
	appCore := zapcore.NewCore(encoder, appWriter, levelRange{min: minLevel, max: zapcore.WarnLevel})
	// Core for error.log: Error..Fatal
	errCore := zapcore.NewCore(encoder, errWriter, levelRange{min: zapcore.ErrorLevel, max: zapcore.FatalLevel})

	core := zapcore.NewTee(appCore, errCore)
	z := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	return &zapLogger{logger: z}, nil
}

// levelRange enables a range of levels
type levelRange struct{ min, max zapcore.Level }

func (l levelRange) Enabled(level zapcore.Level) bool {
	return level >= l.min && level <= l.max
}

// Info logs an informational message
func (z *zapLogger) Info(msg string, fields ...zap.Field) {
	z.logger.Info(msg, fields...)
}

// Error logs an error message
func (z *zapLogger) Error(msg string, fields ...zap.Field) {
	z.logger.Error(msg, fields...)
}

// Debug logs a debug message
func (z *zapLogger) Debug(msg string, fields ...zap.Field) {
	z.logger.Debug(msg, fields...)
}

// Warn logs a warning message
func (z *zapLogger) Warn(msg string, fields ...zap.Field) {
	z.logger.Warn(msg, fields...)
}

// Sync flushes any buffered log entries; swallow sync errors gracefully
func (z *zapLogger) Sync() error {
	if z == nil || z.logger == nil {
		return nil
	}
	if err := z.logger.Sync(); err != nil {
		// Handle typical sync errors gracefully without surfacing to UI
		_ = ignore(err)
		return nil
	}
	return nil
}

// Helper functions for creating fields
func Error(err error) zap.Field {
	return zap.Error(err)
}

func String(key, val string) zap.Field {
	return zap.String(key, val)
}

func Int(key string, val int) zap.Field {
	return zap.Int(key, val)
}

func Any(key string, val interface{}) zap.Field {
	return zap.Any(key, val)
}

// internal helpers
func maxInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func ignore(err error) error {
	// We may choose to examine error strings and return nil for known benign errors
	// Keep placeholder for future filtering.
	return nil
}
