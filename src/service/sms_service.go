package service

import (
	"fmt"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	dypnsapi "github.com/alibabacloud-go/dypnsapi-20170525/v3/client"
	"github.com/alibabacloud-go/tea/tea"

	"bubble/src/logger"
)

// SMSService 阿里云号码认证服务（Dypnsapi — 短信验证码发送与校验）
//
// 使用 SendSmsVerifyCode / CheckSmsVerifyCode 接口：
//   - 阿里云自动生成验证码并发送短信
//   - 阿里云负责存储和校验验证码
//   - 服务端无需本地存储/哈希验证码
type SMSService struct {
	enabled    bool
	client     *dypnsapi.Client
	signName   string // 短信签名
	schemeName string // 认证方案名称
}

// NewSMSService 创建阿里云号码认证服务客户端
func NewSMSService(enabled bool, accessKeyID, accessSecret, signName, schemeName string) *SMSService {
	if !enabled || accessKeyID == "" || accessSecret == "" {
		return &SMSService{enabled: false}
	}

	config := &openapi.Config{
		AccessKeyId:     tea.String(accessKeyID),
		AccessKeySecret: tea.String(accessSecret),
	}
	// Endpoint 参考 https://api.aliyun.com/product/Dypnsapi
	config.Endpoint = tea.String("dypnsapi.aliyuncs.com")

	client, err := dypnsapi.NewClient(config)
	if err != nil {
		logger.Errorf("[SMS] Failed to create Aliyun Dypnsapi client: %v", err)
		return &SMSService{enabled: false}
	}

	return &SMSService{
		enabled:    true,
		client:     client,
		signName:   signName,
		schemeName: schemeName,
	}
}

// SendVerificationCode 发送短信验证码（由阿里云自动生成验证码并发送）
//
// phone: 手机号（国内 11 位号码，如 13800138000）
//
// 阿里云会根据模板中的 ##code## 占位符自动替换为生成的验证码。
// 验证码由阿里云存储和管理，服务端无需保存。
func (s *SMSService) SendVerificationCode(phone string, tpl SMSTemplate) error {
	if !s.enabled {
		logger.Infof("[SMS] SMS disabled, would send verification code to %s (template=%s)", phone, tpl.Code)
		return nil
	}

	if s.client == nil {
		return fmt.Errorf("SMS client not initialized")
	}

	sendReq := &dypnsapi.SendSmsVerifyCodeRequest{
		SchemeName:    tea.String(s.schemeName),
		CountryCode:   tea.String("86"),
		PhoneNumber:   tea.String(phone),
		SignName:      tea.String(s.signName),
		TemplateCode:  tea.String(tpl.Code),
		TemplateParam: tea.String(tpl.Param),
		CodeLength:    tea.Int64(6), // 验证码长度，默认为 6 位
	}

	logger.Infof("[SMS] Sending SMS to %s: signName=%s schemeName=%s templateCode=%s templateParam=%s",
		phone, s.signName, s.schemeName, tpl.Code, tpl.Param)

	resp, err := s.client.SendSmsVerifyCode(sendReq)
	if err != nil {
		logger.Errorf("[SMS] Failed to send SMS to %s: %v", phone, err)
		return fmt.Errorf("发送短信失败: %w", err)
	}

	body := resp.GetBody()
	logger.Infof("[SMS] Response body for %s: %s", phone, body.String())

	if body != nil && body.Code != nil && tea.StringValue(body.Code) != "OK" {
		errMsg := tea.StringValue(body.Message)
		logger.Errorf("[SMS] SMS API error for %s: code=%s message=%s requestId=%s",
			phone, tea.StringValue(body.Code), errMsg, tea.StringValue(body.RequestId))
		return fmt.Errorf("短信发送失败: %s", errMsg)
	}

	logger.Infof("[SMS] Verification code sent to %s successfully", phone)
	return nil
}

// CheckVerificationCode 校验短信验证码（由阿里云校验）
//
// 返回值：
//   - (true, nil)  — 验证通过
//   - (false, nil) — 验证码错误/过期（正常业务失败）
//   - (false, err) — 接口调用异常
func (s *SMSService) CheckVerificationCode(phone, code string) (bool, error) {
	if !s.enabled {
		// 未启用时直接通过（开发/测试模式）
		logger.Infof("[SMS] SMS disabled, auto-pass verification for %s", phone)
		return true, nil
	}

	if s.client == nil {
		return false, fmt.Errorf("SMS client not initialized")
	}

	checkReq := &dypnsapi.CheckSmsVerifyCodeRequest{
		PhoneNumber: tea.String(phone),
		VerifyCode:  tea.String(code),
		SchemeName:  tea.String(s.schemeName),
	}

	resp, err := s.client.CheckSmsVerifyCode(checkReq)
	if err != nil {
		logger.Errorf("[SMS] Failed to check verification code for %s: %v", phone, err)
		return false, fmt.Errorf("验证码校验失败: %w", err)
	}

	if resp.Body == nil {
		return false, fmt.Errorf("验证码校验返回为空")
	}

	if tea.StringValue(resp.Body.Code) != "OK" {
		errMsg := tea.StringValue(resp.Body.Message)
		logger.Warnf("[SMS] Check verify code API error for %s: code=%s message=%s",
			phone, tea.StringValue(resp.Body.Code), errMsg)
		return false, nil
	}

	// 检查 Model.VerifyResult: PASS=通过 / UNKNOWN=失败
	if resp.Body.Model != nil && tea.StringValue(resp.Body.Model.VerifyResult) == "PASS" {
		logger.Infof("[SMS] Verification code check PASSED for %s", phone)
		return true, nil
	}

	logger.Infof("[SMS] Verification code check FAILED for %s", phone)
	return false, nil
}
