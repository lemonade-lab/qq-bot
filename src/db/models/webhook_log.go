package models

import "time"

// WebhookLog 记录机器人 Webhook 调用日志
type WebhookLog struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	RobotID    uint      `json:"robotId" gorm:"index:idx_webhook_log_robot_time"`
	EventType  string    `json:"eventType" gorm:"size:64;index"`  // 事件类型: MESSAGE_CREATE, TEST 等
	URL        string    `json:"url" gorm:"size:512"`             // 请求的 Webhook 地址
	StatusCode int       `json:"statusCode"`                      // HTTP 响应状态码，0 表示请求失败
	Success    bool      `json:"success"`                         // 是否成功 (2xx)
	Error      string    `json:"error,omitempty" gorm:"size:512"` // 错误信息
	LatencyMs  int64     `json:"latencyMs"`                       // 响应耗时（毫秒）
	CreatedAt  time.Time `json:"createdAt" gorm:"index:idx_webhook_log_robot_time"`
}
