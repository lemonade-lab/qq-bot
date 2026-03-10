package logger

import (
	"time"

	"github.com/sirupsen/logrus"
)

// Log types for categorization
const (
	LogTypeHTTP      = "http_request"
	LogTypeDatabase  = "database"
	LogTypeBusiness  = "business"
	LogTypeAuth      = "auth"
	LogTypeWebSocket = "websocket"
	LogTypeSystem    = "system"
	LogTypeExternal  = "external_api"
	LogTypeCache     = "cache"
	LogTypeSecurity  = "security"
)

// LogContext provides common fields for structured logging
type LogContext struct {
	RequestID string
	UserID    string
	Action    string
	Resource  string
	Fields    logrus.Fields
}

// NewLogContext creates a new log context
func NewLogContext(requestID, userID string) *LogContext {
	return &LogContext{
		RequestID: requestID,
		UserID:    userID,
		Fields:    make(logrus.Fields),
	}
}

// WithAction adds action to log context
func (lc *LogContext) WithAction(action string) *LogContext {
	lc.Action = action
	return lc
}

// WithResource adds resource to log context
func (lc *LogContext) WithResource(resource string) *LogContext {
	lc.Resource = resource
	return lc
}

// WithField adds a field to log context
func (lc *LogContext) WithField(key string, value interface{}) *LogContext {
	lc.Fields[key] = value
	return lc
}

// WithFields adds multiple fields to log context
func (lc *LogContext) WithFields(fields logrus.Fields) *LogContext {
	for k, v := range fields {
		lc.Fields[k] = v
	}
	return lc
}

// getEntry returns a logrus entry with context fields
func (lc *LogContext) getEntry(logType string) *logrus.Entry {
	fields := logrus.Fields{
		"type": logType,
	}

	if lc.RequestID != "" {
		fields["request_id"] = lc.RequestID
	}
	if lc.UserID != "" {
		fields["user_id"] = lc.UserID
	}
	if lc.Action != "" {
		fields["action"] = lc.Action
	}
	if lc.Resource != "" {
		fields["resource"] = lc.Resource
	}

	// Merge custom fields
	for k, v := range lc.Fields {
		fields[k] = v
	}

	return Log.WithFields(fields)
}

// Debug logs a debug message with context
func (lc *LogContext) Debug(logType, message string) {
	lc.getEntry(logType).Debug(message)
}

// Info logs an info message with context
func (lc *LogContext) Info(logType, message string) {
	lc.getEntry(logType).Info(message)
}

// Warn logs a warning message with context
func (lc *LogContext) Warn(logType, message string) {
	lc.getEntry(logType).Warn(message)
}

// Error logs an error message with context
func (lc *LogContext) Error(logType, message string) {
	lc.getEntry(logType).Error(message)
}

// === Convenience functions for common log types ===

// LogDatabase logs database operations
func LogDatabase(ctx *LogContext, operation, table string, duration time.Duration, err error) {
	fields := logrus.Fields{
		"operation":   operation,
		"table":       table,
		"duration_ms": float64(duration.Nanoseconds()) / 1e6,
	}

	if ctx != nil {
		ctx.WithFields(fields)
	} else {
		ctx = &LogContext{Fields: fields}
	}

	if err != nil {
		ctx.WithField("error", err.Error()).Error(LogTypeDatabase, "Database operation failed")
	} else {
		ctx.Debug(LogTypeDatabase, "Database operation completed")
	}
}

// LogAuth logs authentication events
func LogAuth(ctx *LogContext, event, username string, success bool, reason string) {
	fields := logrus.Fields{
		"event":    event,
		"username": username,
		"success":  success,
	}

	if reason != "" {
		fields["reason"] = reason
	}

	if ctx != nil {
		ctx.WithFields(fields)
	} else {
		ctx = &LogContext{Fields: fields}
	}

	message := "Authentication event: " + event
	if success {
		ctx.Info(LogTypeAuth, message)
	} else {
		ctx.Warn(LogTypeAuth, message)
	}
}

// LogBusiness logs business logic operations
func LogBusiness(ctx *LogContext, operation, entity string, details logrus.Fields) {
	if ctx == nil {
		ctx = NewLogContext("", "")
	}

	ctx.WithAction(operation).WithResource(entity)

	if details != nil {
		ctx.WithFields(details)
	}

	ctx.Info(LogTypeBusiness, "Business operation: "+operation)
}

// LogExternalAPI logs external API calls
func LogExternalAPI(ctx *LogContext, service, endpoint string, statusCode int, duration time.Duration, err error) {
	fields := logrus.Fields{
		"service":     service,
		"endpoint":    endpoint,
		"status_code": statusCode,
		"duration_ms": float64(duration.Nanoseconds()) / 1e6,
	}

	if ctx != nil {
		ctx.WithFields(fields)
	} else {
		ctx = &LogContext{Fields: fields}
	}

	if err != nil {
		ctx.WithField("error", err.Error()).Error(LogTypeExternal, "External API call failed")
	} else if statusCode >= 400 {
		ctx.Warn(LogTypeExternal, "External API call returned error status")
	} else {
		ctx.Info(LogTypeExternal, "External API call completed")
	}
}

// LogCache logs cache operations
func LogCache(ctx *LogContext, operation, key string, hit bool, err error) {
	fields := logrus.Fields{
		"operation": operation,
		"key":       key,
		"hit":       hit,
	}

	if ctx != nil {
		ctx.WithFields(fields)
	} else {
		ctx = &LogContext{Fields: fields}
	}

	if err != nil {
		ctx.WithField("error", err.Error()).Warn(LogTypeCache, "Cache operation failed")
	} else {
		ctx.Debug(LogTypeCache, "Cache operation completed")
	}
}

// LogSecurity logs security events
func LogSecurity(ctx *LogContext, event string, severity string, details logrus.Fields) {
	if ctx == nil {
		ctx = NewLogContext("", "")
	}

	ctx.WithAction(event).WithField("severity", severity)

	if details != nil {
		ctx.WithFields(details)
	}

	message := "Security event: " + event

	switch severity {
	case "critical", "high":
		ctx.Error(LogTypeSecurity, message)
	case "medium":
		ctx.Warn(LogTypeSecurity, message)
	default:
		ctx.Info(LogTypeSecurity, message)
	}
}

// LogWebSocket logs WebSocket events
func LogWebSocket(ctx *LogContext, event string, connectionID string, details logrus.Fields) {
	if ctx == nil {
		ctx = NewLogContext("", "")
	}

	ctx.WithAction(event).WithField("connection_id", connectionID)

	if details != nil {
		ctx.WithFields(details)
	}

	ctx.Info(LogTypeWebSocket, "WebSocket event: "+event)
}
