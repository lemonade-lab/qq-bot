package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

type LokiHook struct {
	url    string
	client *http.Client
	labels map[string]string
}

// LokiMessage represents a log message for Loki
type LokiMessage struct {
	Streams []LokiStream `json:"streams"`
}

// LokiStream represents a log stream
type LokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

// NewLokiHook creates a new Loki hook for logrus
func NewLokiHook(url string, labels map[string]string) (*LokiHook, error) {
	return &LokiHook{
		url: url,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		labels: labels,
	}, nil
}

// Levels returns the log levels this hook should be triggered for
func (h *LokiHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire sends the log entry to Loki
func (h *LokiHook) Fire(entry *logrus.Entry) error {
	// Create label set
	labelSet := make(map[string]string)
	for k, v := range h.labels {
		labelSet[k] = v
	}
	labelSet["level"] = entry.Level.String()

	// Add caller info if available
	if entry.Caller != nil {
		labelSet["caller"] = fmt.Sprintf("%s:%d", shortFileName(entry.Caller.File), entry.Caller.Line)
	}

	// Add custom fields as labels
	for k, v := range entry.Data {
		if str, ok := v.(string); ok {
			labelSet[k] = str
		}
	}

	// Create log line
	line := entry.Message
	if len(entry.Data) > 0 {
		data, _ := json.Marshal(entry.Data)
		line = fmt.Sprintf("%s | %s", line, string(data))
	}

	// Create Loki message
	timestamp := fmt.Sprintf("%d", entry.Time.UnixNano())
	lokiMsg := LokiMessage{
		Streams: []LokiStream{
			{
				Stream: labelSet,
				Values: [][]string{
					{timestamp, line},
				},
			},
		},
	}

	// Send to Loki (non-blocking)
	go func() {
		data, err := json.Marshal(lokiMsg)
		if err != nil {
			return
		}

		req, err := http.NewRequest("POST", h.url, bytes.NewBuffer(data))
		if err != nil {
			return
		}

		req.Header.Set("Content-Type", "application/json")
		_, _ = h.client.Do(req)
	}()

	return nil
}

// AddLokiHook adds a Loki hook to the logger
func AddLokiHook(lokiURL string, appName, env string) error {
	labels := map[string]string{
		"app": appName,
		"env": env,
	}

	hook, err := NewLokiHook(lokiURL, labels)
	if err != nil {
		return err
	}

	Log.AddHook(hook)
	return nil
}
