package logger

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"blocowallet/internal/terminal"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

var urlPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)
var credentialPattern = regexp.MustCompile(`(?i)(password|passphrase|token|secret|api[_-]?key|authorization|cookie|dsn)=("[^"]*"|'[^']*'|[^\s,;]+)`)

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

func ensurePrivateLogDirectory(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("log directory must be a regular directory")
		}
		return os.Chmod(path, 0700)
	}
	if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err = os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("log directory changed during creation")
	}
	return os.Chmod(path, 0700)
}

func ensurePrivateLogFile(path string) error {
	info, err := os.Lstat(path)
	if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return fmt.Errorf("log path must be a regular file")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// NewFileLogger configures a zap logger to write to rotating log files only (no stderr)
func NewFileLogger(c LoggingConfig) (Logger, error) {
	if c.LogDir == "" {
		return &zapLogger{logger: zap.NewNop()}, nil
	}

	// Ensure log directory exists
	if err := ensurePrivateLogDirectory(c.LogDir); err != nil {
		return nil, err
	}

	appPath := filepath.Join(c.LogDir, "app.log")
	errPath := filepath.Join(c.LogDir, "error.log")

	// Ensure log files exist so tests and tools can rely on their presence even if empty
	if err := ensurePrivateLogFile(appPath); err != nil {
		return nil, err
	}
	if err := ensurePrivateLogFile(errPath); err != nil {
		return nil, err
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
	z.logger.Info(sanitizeLogString(msg), sanitizeLogFields(fields)...)
}

// Error logs an error message
func (z *zapLogger) Error(msg string, fields ...zap.Field) {
	z.logger.Error(sanitizeLogString(msg), sanitizeLogFields(fields)...)
}

// Debug logs a debug message
func (z *zapLogger) Debug(msg string, fields ...zap.Field) {
	z.logger.Debug(sanitizeLogString(msg), sanitizeLogFields(fields)...)
}

// Warn logs a warning message
func (z *zapLogger) Warn(msg string, fields ...zap.Field) {
	z.logger.Warn(sanitizeLogString(msg), sanitizeLogFields(fields)...)
}

// Sync flushes any buffered log entries; swallow sync errors gracefully
func (z *zapLogger) Sync() error {
	if z == nil || z.logger == nil {
		return nil
	}
	return z.logger.Sync()
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

func sanitizeLogFields(fields []zap.Field) []zap.Field {
	sanitized := make([]zap.Field, 0, len(fields))
	for _, field := range fields {
		key := terminal.SanitizeInline(field.Key, 64)
		lowerKey := strings.ToLower(key)
		if strings.Contains(lowerKey, "password") || strings.Contains(lowerKey, "passphrase") || strings.Contains(lowerKey, "token") || strings.Contains(lowerKey, "secret") || strings.Contains(lowerKey, "mnemonic") || strings.Contains(lowerKey, "private") || strings.Contains(lowerKey, "seed") || strings.Contains(lowerKey, "dsn") || strings.Contains(lowerKey, "authorization") || strings.Contains(lowerKey, "credential") || strings.Contains(lowerKey, "cookie") || strings.Contains(lowerKey, "session") || strings.Contains(lowerKey, "signature") || strings.Contains(lowerKey, "api_key") {
			sanitized = append(sanitized, zap.String(key, "[REDACTED]"))
			continue
		}
		switch field.Type {
		case zapcore.StringType:
			sanitized = append(sanitized, zap.String(key, sanitizeLogString(field.String)))
		case zapcore.ErrorType:
			if err, ok := field.Interface.(error); ok {
				sanitized = append(sanitized, zap.String(key, sanitizeLogString(err.Error())))
			} else {
				sanitized = append(sanitized, zap.String(key, "[invalid error]"))
			}
		case zapcore.StringerType:
			sanitized = append(sanitized, zap.String(key, sanitizeLogString(fmt.Sprint(field.Interface))))
		case zapcore.ByteStringType:
			if value, ok := field.Interface.([]byte); ok {
				sanitized = append(sanitized, zap.String(key, sanitizeLogString(string(value))))
			}
		case zapcore.BinaryType:
			sanitized = append(sanitized, zap.String(key, "[binary value omitted]"))
		case zapcore.ArrayMarshalerType, zapcore.ObjectMarshalerType, zapcore.InlineMarshalerType, zapcore.ReflectType:
			sanitized = append(sanitized, zap.String(key, "[structured value omitted]"))
		default:
			field.Key = key
			sanitized = append(sanitized, field)
		}
	}
	return sanitized
}

func sanitizeLogString(value string) string {
	sanitized := terminal.SanitizeInline(value, 2048)
	redactedURLs := urlPattern.ReplaceAllStringFunc(sanitized, func(candidate string) string {
		trimmed := strings.TrimRight(candidate, ").,;]")
		suffix := candidate[len(trimmed):]
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return "[REDACTED URL]" + suffix
		}
		return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host) + suffix
	})
	return credentialPattern.ReplaceAllString(redactedURLs, "$1=[REDACTED]")
}

// internal helpers
func maxInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
