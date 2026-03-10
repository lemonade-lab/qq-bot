package service

// ──────────────────────────────────────────────
// SMS Templates — 阿里云号码认证服务短信模板
// ──────────────────────────────────────────────
//
// 模板变量说明：
//   ${code} — 验证码（由阿里云自动生成，使用 ##code## 占位符）
//   ${min}  — 验证码有效分钟数
//
// 模板参数统一格式：
//   {"code":"##code##","min":"5"}

// SMSTemplate 短信模板
type SMSTemplate struct {
	Code  string // 模板编号
	Param string // 模板参数 JSON
}

// 验证码有效期（分钟），所有模板统一使用
const smsCodeExpireMinutes = "5"

// 短信模板定义
var (
	// SMSTemplateLogin 登录/注册验证码模板
	// 内容：您的验证码为${code}。尊敬的客户，以上验证码${min}分钟内有效，请注意保密，切勿告知他人。
	SMSTemplateLogin = SMSTemplate{
		Code:  "100001",
		Param: `{"code":"##code##","min":"` + smsCodeExpireMinutes + `"}`,
	}

	// SMSTemplateChangePhone 修改绑定手机号验证码模板
	// 内容：尊敬的客户，您正在进行修改手机号操作，您的验证码为${code}。以上验证码${min}分钟内有效，请注意保密，切勿告知他人。
	SMSTemplateChangePhone = SMSTemplate{
		Code:  "100002",
		Param: `{"code":"##code##","min":"` + smsCodeExpireMinutes + `"}`,
	}

	// SMSTemplateResetPassword 重置密码验证码模板
	// 内容：尊敬的客户，您正在进行重置密码操作，您的验证码为${code}。以上验证码${min}分钟内有效，请注意保密，切勿告知他人。
	SMSTemplateResetPassword = SMSTemplate{
		Code:  "100003",
		Param: `{"code":"##code##","min":"` + smsCodeExpireMinutes + `"}`,
	}

	// SMSTemplateBindPhone 绑定新手机号验证码模板
	// 内容：尊敬的客户，您正在进行绑定手机号操作，您的验证码为${code}。以上验证码${min}分钟内有效，请注意保密，切勿告知他人。
	SMSTemplateBindPhone = SMSTemplate{
		Code:  "100004",
		Param: `{"code":"##code##","min":"` + smsCodeExpireMinutes + `"}`,
	}

	// SMSTemplateVerifyPhone 验证绑定手机号验证码模板
	// 内容：尊敬的客户，您正在验证绑定手机号操作，您的验证码为${code}。以上验证码${min}分钟内有效，请注意保密，切勿告知他人。
	SMSTemplateVerifyPhone = SMSTemplate{
		Code:  "100005",
		Param: `{"code":"##code##","min":"` + smsCodeExpireMinutes + `"}`,
	}
)
