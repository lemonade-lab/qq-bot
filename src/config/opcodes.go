package config

// Gateway Op Codes centralized for both user and bot protocols
const (
	// Core
	OpDispatch     = 0
	OpHeartbeat    = 1
	OpHello        = 10
	OpHeartbeatAck = 11

	// Subscription
	OpSubscribe   = 30
	OpUnsubscribe = 31

	// Voice signaling (generic)
	OpVoiceSignal = 40

	// LiveKit / Channel media ops
	OpChannelVoiceState  = 50
	OpChannelVoiceJoin   = 51
	OpChannelVoiceLeave  = 52
	OpChannelVoiceUpdate = 53
	OpChannelVideoState  = 54
	OpChannelScreenShare = 55

	// DM call
	OpDmCallStart  = 60
	OpDmCallAccept = 61
	OpDmCallReject = 62
	OpDmCallCancel = 63

	// QR Code Login
	OpQRCodeRequest   = 70 // 客户端请求生成二维码
	OpQRCodeGenerated = 71 // 服务端返回二维码数据
	OpQRCodeScanned   = 72 // 服务端通知二维码已被扫描
	OpQRCodeConfirmed = 73 // 服务端通知二维码已确认，返回认证信息
	OpQRCodeExpired   = 74 // 服务端通知二维码已过期
	OpQRCodeCancelled = 75 // 服务端通知二维码已取消
)

// 服务器权限常量
const (
	PermManageFiles uint64 = 1 << 11 // 文件管理权限
)
