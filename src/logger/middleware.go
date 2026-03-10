package logger

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

const (
	// Maximum body size to log (100KB)
	maxBodyLogSize = 100 * 1024
)

// responseBodyWriter wraps gin.ResponseWriter to capture response body
type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *responseBodyWriter) Write(b []byte) (int, error) {
	// Write to buffer if not too large
	if w.body.Len() < maxBodyLogSize {
		w.body.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// GinLogger is a middleware that logs HTTP requests using logrus
func GinLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Start timer
		startTime := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// Get request ID
		requestID := GetRequestID(c)

		// Read request body for logging (if applicable)
		var requestBody string
		if shouldLogBody(c.Request.Method, path) {
			if c.Request.Body != nil {
				bodyBytes, _ := io.ReadAll(c.Request.Body)
				// Restore the body for handlers
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

				if len(bodyBytes) > 0 && len(bodyBytes) < maxBodyLogSize {
					requestBody = sanitizeBody(bodyBytes)
				}
			}
		}

		// Wrap response writer to capture response body
		blw := &responseBodyWriter{
			ResponseWriter: c.Writer,
			body:           bytes.NewBufferString(""),
		}
		c.Writer = blw

		// Process request
		c.Next()

		// Calculate latency
		latency := time.Since(startTime)
		latencyMs := float64(latency.Nanoseconds()) / 1e6

		// Get request info
		clientIP := c.ClientIP()
		method := c.Request.Method
		statusCode := c.Writer.Status()
		errorMessage := c.Errors.ByType(gin.ErrorTypePrivate).String()
		bodySize := c.Writer.Size()

		if raw != "" {
			path = path + "?" + raw
		}

		// Get user ID from context if available
		userID := ""
		if uid, exists := c.Get("user_id"); exists {
			if id, ok := uid.(string); ok {
				userID = id
			}
		}

		// Create log entry with fields
		fields := logrus.Fields{
			"type":       "http_request",
			"request_id": requestID,
			"status":     statusCode,
			"method":     method,
			"path":       path,
			"ip":         clientIP,
			"latency_ms": latencyMs,
			"latency":    latency.String(),
			"user_agent": c.Request.UserAgent(),
			"body_size":  bodySize,
			"protocol":   c.Request.Proto,
		}

		// Add user ID if available
		if userID != "" {
			fields["user_id"] = userID
		}

		// Add request body for non-GET requests (sanitized)
		if requestBody != "" {
			fields["request_body"] = requestBody
		}

		// Add response body for errors or if body is small
		if statusCode >= 400 && blw.body.Len() > 0 && blw.body.Len() < maxBodyLogSize {
			fields["response_body"] = sanitizeBody(blw.body.Bytes())
		}

		entry := Log.WithFields(fields)

		// Log based on status code
		if len(errorMessage) > 0 {
			entry = entry.WithField("error", errorMessage)
		}

		// Add custom message based on status
		message := getLogMessage(method, path, statusCode, latencyMs)

		switch {
		case statusCode >= 500:
			entry.Error(message)
		case statusCode >= 400:
			entry.Warn(message)
		case statusCode >= 300:
			entry.Info(message)
		default:
			// Only log successful requests at debug level if they're fast
			if latencyMs > 1000 {
				entry.Warn(message) // Slow request
			} else if latencyMs > 500 {
				entry.Info(message) // Moderate request
			} else {
				entry.Debug(message) // Fast request
			}
		}
	}
}

// GinRecovery is a middleware that recovers from panics and logs them
func GinRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Get request ID
				requestID := GetRequestID(c)

				// Get user ID if available
				userID := ""
				if uid, exists := c.Get("user_id"); exists {
					if id, ok := uid.(string); ok {
						userID = id
					}
				}

				fields := logrus.Fields{
					"type":       "panic",
					"request_id": requestID,
					"error":      err,
					"path":       c.Request.URL.Path,
					"method":     c.Request.Method,
					"ip":         c.ClientIP(),
				}

				if userID != "" {
					fields["user_id"] = userID
				}

				Log.WithFields(fields).Error("Panic recovered")

				c.AbortWithStatus(500)
			}
		}()
		c.Next()
	}
}

// shouldLogBody determines if request body should be logged
func shouldLogBody(method, path string) bool {
	// Don't log GET, HEAD, OPTIONS requests
	if method == "GET" || method == "HEAD" || method == "OPTIONS" {
		return false
	}

	// Don't log file upload endpoints
	if strings.Contains(path, "/upload") || strings.Contains(path, "/files") {
		return false
	}

	return true
}

// sanitizeBody sanitizes request/response body for logging
func sanitizeBody(body []byte) string {
	// Try to parse as JSON
	var jsonBody interface{}
	if err := json.Unmarshal(body, &jsonBody); err == nil {
		// If it's JSON, sanitize sensitive fields
		if m, ok := jsonBody.(map[string]interface{}); ok {
			sanitizeMap(m)
		}
		sanitized, _ := json.Marshal(jsonBody)
		return string(sanitized)
	}

	// If not JSON, return as string but truncate if too long
	str := string(body)
	if len(str) > 1000 {
		return str[:1000] + "...[truncated]"
	}
	return str
}

// sanitizeMap removes sensitive information from map
func sanitizeMap(m map[string]interface{}) {
	sensitiveKeys := []string{
		"password", "token", "secret", "api_key", "apikey",
		"access_token", "refresh_token", "authorization",
		"credit_card", "cvv", "ssn",
	}

	for key, value := range m {
		keyLower := strings.ToLower(key)

		// Check if key is sensitive
		for _, sensitiveKey := range sensitiveKeys {
			if strings.Contains(keyLower, sensitiveKey) {
				m[key] = "[REDACTED]"
				break
			}
		}

		// Recursively sanitize nested maps
		if nested, ok := value.(map[string]interface{}); ok {
			sanitizeMap(nested)
		}

		// Sanitize arrays of maps
		if arr, ok := value.([]interface{}); ok {
			for _, item := range arr {
				if nestedMap, ok := item.(map[string]interface{}); ok {
					sanitizeMap(nestedMap)
				}
			}
		}
	}
}

// getLogMessage generates a descriptive log message
func getLogMessage(method, path string, status int, latencyMs float64) string {
	return "HTTP Request"
}
