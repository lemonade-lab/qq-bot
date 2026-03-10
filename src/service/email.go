package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// EmailService handles email sending via Tencent Cloud SES
type EmailService struct {
	enabled    bool
	secretId   string
	secretKey  string
	region     string
	templateID uint
	from       string
	fromName   string
}

// NewEmailService creates a new email service for Tencent Cloud SES
func NewEmailService(enabled bool, secretId, secretKey, region string, templateID uint, from, fromName string) *EmailService {
	return &EmailService{
		enabled:    enabled,
		secretId:   secretId,
		secretKey:  secretKey,
		region:     region,
		templateID: templateID,
		from:       from,
		fromName:   fromName,
	}
}

// TC3 签名相关常量
const (
	algorithm   = "TC3-HMAC-SHA256"
	service     = "ses"
	host        = "ses.tencentcloudapi.com"
	contentType = "application/json"
	action      = "SendEmail"
	version     = "2020-10-02"
)

// sha256hex 计算 SHA256 哈希
func sha256hex(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

// hmacsha256 计算 HMAC-SHA256
func hmacsha256(key, data string) []byte {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(data))
	return h.Sum(nil)
}

// getTC3Authorization 生成腾讯云 TC3-HMAC-SHA256 签名
func (e *EmailService) getTC3Authorization(payload string, timestamp int64) string {
	// 1. 拼接规范请求串
	httpRequestMethod := "POST"
	canonicalURI := "/"
	canonicalQueryString := ""
	canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\n", contentType, host)
	signedHeaders := "content-type;host"
	hashedRequestPayload := sha256hex(payload)
	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		httpRequestMethod,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		hashedRequestPayload)

	// 2. 拼接待签名字符串
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	hashedCanonicalRequest := sha256hex(canonicalRequest)
	stringToSign := fmt.Sprintf("%s\n%d\n%s\n%s",
		algorithm,
		timestamp,
		credentialScope,
		hashedCanonicalRequest)

	// 3. 计算签名
	secretDate := hmacsha256("TC3"+e.secretKey, date)
	secretService := hmacsha256(string(secretDate), service)
	secretSigning := hmacsha256(string(secretService), "tc3_request")
	signature := hex.EncodeToString(hmacsha256(string(secretSigning), stringToSign))

	// 4. 拼接 Authorization
	authorization := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm,
		e.secretId,
		credentialScope,
		signedHeaders,
		signature)

	return authorization
}

// SendEmailWithTemplate 使用腾讯云 SES 模板发送邮件
func (e *EmailService) SendEmailWithTemplate(to, account, code string, validMinutes int) error {
	if !e.enabled {
		fmt.Printf("[Email] Would send to %s: code=%s\n", to, code)
		return nil
	}

	if e.secretId == "" || e.secretKey == "" {
		return fmt.Errorf("腾讯云密钥未配置")
	}

	if e.templateID == 0 {
		return fmt.Errorf("邮件模板ID未配置")
	}

	timestamp := time.Now().Unix()

	// 构建请求体
	type TemplateData struct {
		Account string `json:"account"`
		Code    string `json:"code"`
		Time    string `json:"time"`
	}

	type Template struct {
		TemplateID   uint   `json:"TemplateID"`
		TemplateData string `json:"TemplateData"`
	}

	type RequestBody struct {
		FromEmailAddress string   `json:"FromEmailAddress"`
		Destination      []string `json:"Destination"`
		Subject          string   `json:"Subject"`
		Template         Template `json:"Template"`
	}

	templateDataJSON, _ := json.Marshal(TemplateData{
		Account: account,
		Code:    code,
		Time:    strconv.Itoa(validMinutes),
	})

	reqBody := RequestBody{
		FromEmailAddress: e.from,
		Destination:      []string{to},
		Subject:          "安全验证",
		Template: Template{
			TemplateID:   e.templateID,
			TemplateData: string(templateDataJSON),
		},
	}

	payloadBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("构建请求体失败: %w", err)
	}
	payload := string(payloadBytes)

	// 生成签名
	authorization := e.getTC3Authorization(payload, timestamp)

	// 发送请求
	url := fmt.Sprintf("https://%s", host)
	req, err := http.NewRequest("POST", url, bytes.NewBufferString(payload))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Host", host)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-TC-Version", version)
	req.Header.Set("X-TC-Region", e.region)

	// 发送请求
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("发送邮件请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	// 检查响应状态
	if resp.StatusCode != 200 {
		return fmt.Errorf("发送邮件失败 (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	// 解析响应
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

	// 检查是否有错误
	if errResp, ok := result["Response"].(map[string]interface{}); ok {
		if errObj, hasErr := errResp["Error"]; hasErr {
			return fmt.Errorf("腾讯云SES错误: %v", errObj)
		}
	}

	fmt.Printf("[Email] ✓ 邮件已发送到 %s\n", to)
	return nil
}

// SendEmail 兼容方法（不推荐使用，保持向后兼容）
func (e *EmailService) SendEmail(to, subject, body string) error {
	if !e.enabled {
		fmt.Printf("[Email] Would send to %s: %s\n", to, subject)
		return nil
	}
	// 腾讯云 SES 不支持自定义HTML，需要使用模板
	return fmt.Errorf("请使用 SendEmailWithTemplate 方法发送邮件")
}

// SendLoginVerificationEmail 发送登录验证码邮件
func (e *EmailService) SendLoginVerificationEmail(to, code string) error {
	return e.SendEmailWithTemplate(to, "用户", code, 5)
}

// SendVerificationEmail 发送邮箱验证码邮件
func (e *EmailService) SendVerificationEmail(to, code string) error {
	return e.SendEmailWithTemplate(to, "用户", code, 5) // 5分钟
}

// SendPasswordResetEmail 发送密码重置邮件
func (e *EmailService) SendPasswordResetEmail(to, code string) error {
	return e.SendEmailWithTemplate(to, "用户", code, 5) // 5分钟
}

// SendMagicLoginEmail 发送邮箱登录即注册验证码邮件
func (e *EmailService) SendMagicLoginEmail(to, code string) error {
	return e.SendEmailWithTemplate(to, "用户", code, 5) // 5分钟
}

// SendWelcomeEmail 发送欢迎邮件
func (e *EmailService) SendWelcomeEmail(to, name string) error {
	// 欢迎邮件使用相同的模板，code 设置为 "WELCOME"
	return e.SendEmailWithTemplate(to, name, "欢迎加入Bubble", 0)
}
