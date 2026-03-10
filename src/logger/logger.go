package logger

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/sirupsen/logrus"
)

var Log *logrus.Logger

// Init initializes the global logger instance
func Init(logLevel string, jsonFormat bool, enableLoki bool, environment string) {
	Log = logrus.New()

	// Set log level based on environment
	level := getLogLevel(logLevel, environment)
	Log.SetLevel(level)

	// Set output format
	if jsonFormat {
		Log.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime:  "timestamp",
				logrus.FieldKeyLevel: "level",
				logrus.FieldKeyMsg:   "message",
				logrus.FieldKeyFunc:  "caller",
			},
			PrettyPrint: environment == "development",
		})
	} else {
		Log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02 15:04:05",
			ForceColors:     true,
		})
	}

	// Report caller information (more verbose in development)
	if environment == "development" || level == logrus.DebugLevel {
		Log.SetReportCaller(true)
		// Custom caller formatter
		Log.SetFormatter(&CustomFormatter{
			Formatter: Log.Formatter,
		})
	}

	// Set output to stdout
	Log.SetOutput(os.Stdout)

	// Log initialization info
	Log.WithFields(logrus.Fields{
		"environment":  environment,
		"log_level":    level.String(),
		"json_format":  jsonFormat,
		"loki_enabled": enableLoki,
	}).Info("Logger initialized")
}

// getLogLevel determines the appropriate log level based on environment
func getLogLevel(configLevel, environment string) logrus.Level {
	// Parse configured level
	level, err := logrus.ParseLevel(configLevel)
	if err != nil {
		level = logrus.InfoLevel
	}

	// Override based on environment if needed
	switch environment {
	case "development", "dev":
		// Development: default to debug if not explicitly set
		if configLevel == "" {
			return logrus.DebugLevel
		}
	case "production", "prod":
		// Production: never go below info level for security
		if level < logrus.InfoLevel {
			Log.Warnf("Log level %s is too verbose for production, using info level", level)
			return logrus.InfoLevel
		}
	}

	return level
}

// CustomFormatter wraps the default formatter to customize caller info
type CustomFormatter struct {
	Formatter logrus.Formatter
}

// Format implements the logrus.Formatter interface
func (f *CustomFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	// Customize the caller field to show file:line instead of full path
	if entry.Caller != nil {
		entry.Data["caller"] = fmt.Sprintf("%s:%d", shortFileName(entry.Caller.File), entry.Caller.Line)
	}
	return f.Formatter.Format(entry)
}

// shortFileName extracts just the filename from the full path
func shortFileName(file string) string {
	parts := strings.Split(file, "/")
	if len(parts) > 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return file
}

// WithField creates a new log entry with a single field
func WithField(key string, value interface{}) *logrus.Entry {
	return Log.WithField(key, value)
}

// WithFields creates a new log entry with multiple fields
func WithFields(fields logrus.Fields) *logrus.Entry {
	return Log.WithFields(fields)
}

// Debug logs a debug message
func Debug(args ...interface{}) {
	Log.Debug(args...)
}

// Debugf logs a formatted debug message
func Debugf(format string, args ...interface{}) {
	Log.Debugf(format, args...)
}

// Info logs an info message
func Info(args ...interface{}) {
	Log.Info(args...)
}

// Infof logs a formatted info message
func Infof(format string, args ...interface{}) {
	Log.Infof(format, args...)
}

// Warn logs a warning message
func Warn(args ...interface{}) {
	Log.Warn(args...)
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...interface{}) {
	Log.Warnf(format, args...)
}

// Error logs an error message
func Error(args ...interface{}) {
	Log.Error(args...)
}

// Errorf logs a formatted error message
func Errorf(format string, args ...interface{}) {
	Log.Errorf(format, args...)
}

// Fatal logs a fatal message and exits
func Fatal(args ...interface{}) {
	Log.Fatal(args...)
}

// Fatalf logs a formatted fatal message and exits
func Fatalf(format string, args ...interface{}) {
	Log.Fatalf(format, args...)
}

// Panic logs a panic message and panics
func Panic(args ...interface{}) {
	Log.Panic(args...)
}

// Panicf logs a formatted panic message and panics
func Panicf(format string, args ...interface{}) {
	Log.Panicf(format, args...)
}

// GetCaller returns the file and line of the caller
func GetCaller() string {
	_, file, line, ok := runtime.Caller(2)
	if !ok {
		return "unknown"
	}
	return fmt.Sprintf("%s:%d", shortFileName(file), line)
}
