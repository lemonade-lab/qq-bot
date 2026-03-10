package config

// 统一管理 Gateway 事件常量与集合

// ========= Gateway 通用常量（消除魔法数字/字符串） =========
// 心跳与速率限制
const (
	DefaultHeartbeatInterval = 30 // 秒
	UserRateLimitRPS         = 5.0
	UserRateLimitBurst       = 10.0
	BotRateLimitRPS          = 10.0
	BotRateLimitBurst        = 20.0
	// READY 初始加载数量上限
	DefaultReadyFriendsLimit   = 100
	DefaultReadyDmThreadsLimit = 100
	// QR Code 二维码相关常量（不同类型有不同过期时间）
	QRCodeLoginExpireSeconds   = 300     // 登录二维码：5分钟
	QRCodeUserExpireSeconds    = 86400   // 用户卡片：24小时
	QRCodeChannelExpireSeconds = 86400   // 频道卡片：24小时
	QRCodeGuildExpireSeconds   = 2592000 // 服务器邀请：30天
	QRCodeCheckInterval        = 2       // 客户端轮询间隔（秒）
)

// Redis Topic 前缀
const (
	TopicPrefixChannel = "chan:"
	TopicPrefixDM      = "dm:"
	TopicPrefixUser    = "user:"
	TopicPrefixGroup   = "group:"
	TopicPrefixGuild   = "guild:"
)

// 业务事件（频道消息/私聊消息/成员事件）
const (
	EventMessageCreate         = "MESSAGE_CREATE"
	EventMessageUpdate         = "MESSAGE_UPDATE"
	EventMessageDelete         = "MESSAGE_DELETE"
	EventMessageUnpin          = "MESSAGE_UNPIN"
	EventMessagePin            = "MESSAGE_PIN"
	EventMessageReactionAdd    = "MESSAGE_REACTION_ADD"
	EventMessageReactionRemove = "MESSAGE_REACTION_REMOVE"
	EventDmMessageCreate       = "DM_MESSAGE_CREATE"
	EventDmMessageUpdate       = "DM_MESSAGE_UPDATE"
	EventDmMessageDelete       = "DM_MESSAGE_DELETE"
	EventDmMessageUnpin        = "DM_MESSAGE_UNPIN"
	EventDmMessagePin          = "DM_MESSAGE_PIN"
	// 群聊事件
	EventGroupMessageCreate      = "GROUP_MESSAGE_CREATE"
	EventGroupMessageUpdate      = "GROUP_MESSAGE_UPDATE"
	EventGroupMessageDelete      = "GROUP_MESSAGE_DELETE"
	EventGroupMessagePin         = "GROUP_MESSAGE_PIN"
	EventGroupMessageUnpin       = "GROUP_MESSAGE_UNPIN"
	EventGroupThreadUpdate       = "GROUP_THREAD_UPDATE"
	EventGroupMemberAdd          = "GROUP_MEMBER_ADD"
	EventGroupMemberRemove       = "GROUP_MEMBER_REMOVE"
	EventGroupAnnouncementCreate = "GROUP_ANNOUNCEMENT_CREATE"
	EventGroupAnnouncementUpdate = "GROUP_ANNOUNCEMENT_UPDATE"
	EventGroupAnnouncementDelete = "GROUP_ANNOUNCEMENT_DELETE"
	// 子房间事件
	EventSubRoomMessageCreate = "SUBROOM_MESSAGE_CREATE"
	EventSubRoomMessageUpdate = "SUBROOM_MESSAGE_UPDATE"
	EventSubRoomMessageDelete = "SUBROOM_MESSAGE_DELETE"
	EventSubRoomUpdate        = "SUBROOM_UPDATE"
	EventSubRoomMemberAdd     = "SUBROOM_MEMBER_ADD"
	EventSubRoomMemberRemove  = "SUBROOM_MEMBER_REMOVE"
	EventGuildMemberAdd       = "GUILD_MEMBER_ADD"
	EventGuildMemberUpdate    = "GUILD_MEMBER_UPDATE"
	EventGuildMemberRemove    = "GUILD_MEMBER_REMOVE"
	// 用户就绪事件
	EventReady     = "READY"
	EventReadySync = "READY_SYNC"
	// 私聊语音事件（用户侧）
	EventVoiceCallIncoming = "VOICE_CALL_INCOMING"
	EventVoiceCallOffer    = "VOICE_CALL_OFFER"
	EventVoiceCallAnswer   = "VOICE_CALL_ANSWER"
	EventVoiceCallICE      = "VOICE_CALL_ICE"
	EventVoiceCallHangup   = "VOICE_CALL_HANGUP"
	// 语音状态与呼入通知（用户侧，现有前端使用）
	EventChannelVoiceState = "CHANNEL_VOICE_STATE"
	EventDmVoiceState      = "DM_VOICE_STATE"
	EventDmCallIncoming    = "DM_CALL_INCOMING"
	EventDmCallAccepted    = "DM_CALL_ACCEPTED"
	EventDmCallRejected    = "DM_CALL_REJECTED"
	EventDmCallCancelled   = "DM_CALL_CANCELLED"
	// 个人级通知与申请（新增）
	EventMentionCreate     = "MENTION_CREATE"
	EventApplicationCreate = "APPLICATION_CREATE"
	EventApplicationUpdate = "APPLICATION_UPDATE"
	// 别名：通用个人通知与申请（默认订阅，按用户路由）
	EventNoticeCreate = "NOTICE_CREATE"
	EventNoticeUpdate = "NOTICE_UPDATE"
	EventApplyCreate  = "APPLY_CREATE"
	EventApplyUpdate  = "APPLY_UPDATE"
	// 红点系统事件
	EventReadStateUpdate = "READ_STATE_UPDATE"
)

// 控制/协议事件（不受机器人事件白名单过滤）
const (
	EventBotReady           = "BOT_READY"
	EventEventsSubscribed   = "EVENTS_SUBSCRIBED"
	EventEventsUnsubscribed = "EVENTS_UNSUBSCRIBED"
	EventSubscribeDenied    = "SUBSCRIBE_DENIED"
	// 当用户连接时自动订阅其私聊线程的通知事件
	EventDmSubscribedOnConnect = "DM_SUBSCRIBED_ON_CONNECT"
)

// 机器人可订阅的业务事件列表
var Events = []string{
	EventMessageCreate,
	EventMessageUpdate,
	EventMessageDelete,
	EventMessageUnpin,
	EventMessagePin,
	EventMessageReactionAdd,
	EventMessageReactionRemove,
	EventDmMessageCreate,
	EventDmMessageUpdate,
	EventDmMessageDelete,
	EventDmMessageUnpin,
	EventDmMessagePin,
	EventGroupMessageCreate,
	EventGroupMessageUpdate,
	EventGroupMessageDelete,
	EventGroupMessagePin,
	EventGroupMessageUnpin,
	EventGroupThreadUpdate,
	EventGroupMemberAdd,
	EventGroupMemberRemove,
	EventGuildMemberAdd,
	EventGuildMemberUpdate,
	EventGuildMemberRemove,
	EventMentionCreate,
	EventApplicationCreate,
	EventApplicationUpdate,
	EventNoticeCreate,
	EventNoticeUpdate,
	EventApplyCreate,
	EventApplyUpdate,
	EventReadStateUpdate,
}

// 业务事件快速判断
var EventsMap = map[string]bool{
	EventMessageCreate:         true,
	EventMessageUpdate:         true,
	EventMessageDelete:         true,
	EventMessageUnpin:          true,
	EventMessagePin:            true,
	EventMessageReactionAdd:    true,
	EventMessageReactionRemove: true,
	EventDmMessageCreate:       true,
	EventDmMessageUpdate:       true,
	EventDmMessageDelete:       true,
	EventDmMessageUnpin:        true,
	EventDmMessagePin:          true,
	EventGroupMessageCreate:    true,
	EventGroupMessageUpdate:    true,
	EventGroupMessageDelete:    true,
	EventGroupMessagePin:       true,
	EventGroupMessageUnpin:     true,
	EventGroupThreadUpdate:     true,
	EventGroupMemberAdd:        true,
	EventGroupMemberRemove:     true,
	EventSubRoomMessageCreate:  true,
	EventSubRoomMessageUpdate:  true,
	EventSubRoomMessageDelete:  true,
	EventSubRoomUpdate:         true,
	EventSubRoomMemberAdd:      true,
	EventSubRoomMemberRemove:   true,
	EventGuildMemberAdd:        true,
	EventGuildMemberUpdate:     true,
	EventGuildMemberRemove:     true,
	EventMentionCreate:         true,
	EventApplicationCreate:     true,
	EventApplicationUpdate:     true,
	EventNoticeCreate:          true,
	EventNoticeUpdate:          true,
	EventApplyCreate:           true,
	EventApplyUpdate:           true,
	EventReadStateUpdate:       true,
}

// 控制/协议事件白名单（机器人默认可接收）
var SpecialEventsMap = map[string]bool{
	EventBotReady:           true,
	EventEventsSubscribed:   true,
	EventEventsUnsubscribed: true,
	EventSubscribeDenied:    true,
}

// 用户侧可接收的事件集合（用于文档/校验/前后端对齐）
var UserEvents = []string{
	EventReady,
	EventReadySync,
	// 文本与成员相关
	EventMessageCreate,
	EventMessageUpdate,
	EventMessageDelete,
	EventMessageUnpin,
	EventMessagePin,
	EventMessageReactionAdd,
	EventMessageReactionRemove,
	EventDmMessageCreate,
	EventDmMessageUpdate,
	EventDmMessageDelete,
	EventDmMessageUnpin,
	EventDmMessagePin,
	EventGroupMessageCreate,
	EventGroupMessageUpdate,
	EventGroupMessageDelete,
	EventGroupMessagePin,
	EventGroupMessageUnpin,
	EventGroupThreadUpdate,
	EventGroupMemberAdd,
	EventGroupMemberRemove,
	EventSubRoomMessageCreate,
	EventSubRoomMessageUpdate,
	EventSubRoomMessageDelete,
	EventSubRoomUpdate,
	EventSubRoomMemberAdd,
	EventSubRoomMemberRemove,
	EventGuildMemberAdd,
	EventGuildMemberUpdate,
	EventGuildMemberRemove,
	// 私聊语音相关
	EventVoiceCallIncoming,
	EventVoiceCallOffer,
	EventVoiceCallAnswer,
	EventVoiceCallICE,
	EventVoiceCallHangup,
	// 语音状态与呼入通知
	EventChannelVoiceState,
	EventDmVoiceState,
	EventDmCallIncoming,
	EventDmCallAccepted,
	EventDmCallRejected,
	EventDmCallCancelled,
	// 个人级通知
	EventMentionCreate,
	EventNoticeCreate,
	EventNoticeUpdate,
	// 申请相关
	EventApplicationCreate,
	EventApplicationUpdate,
	EventApplyCreate,
	EventApplyUpdate,
	// 红点系统
	EventReadStateUpdate,
}

var UserEventsMap = map[string]bool{
	EventReady:                 true,
	EventReadySync:             true,
	EventMessageCreate:         true,
	EventMessageUpdate:         true,
	EventMessageDelete:         true,
	EventMessageUnpin:          true,
	EventMessagePin:            true,
	EventMessageReactionAdd:    true,
	EventMessageReactionRemove: true,
	EventDmMessageCreate:       true,
	EventDmMessageUpdate:       true,
	EventDmMessageDelete:       true,
	EventDmMessageUnpin:        true,
	EventDmMessagePin:          true,
	EventGroupMessageCreate:    true,
	EventGroupMessageUpdate:    true,
	EventGroupMessageDelete:    true,
	EventGroupMessagePin:       true,
	EventGroupMessageUnpin:     true,
	EventGroupThreadUpdate:     true,
	EventGroupMemberAdd:        true,
	EventGroupMemberRemove:     true,
	EventSubRoomMessageCreate:  true,
	EventSubRoomMessageUpdate:  true,
	EventSubRoomMessageDelete:  true,
	EventSubRoomUpdate:         true,
	EventSubRoomMemberAdd:      true,
	EventSubRoomMemberRemove:   true,
	EventGuildMemberAdd:        true,
	EventGuildMemberUpdate:     true,
	EventGuildMemberRemove:     true,
	EventVoiceCallIncoming:     true,
	EventVoiceCallOffer:        true,
	EventVoiceCallAnswer:       true,
	EventVoiceCallICE:          true,
	EventVoiceCallHangup:       true,
	EventChannelVoiceState:     true,
	EventDmVoiceState:          true,
	EventDmCallIncoming:        true,
	EventDmCallAccepted:        true,
	EventDmCallRejected:        true,
	EventDmCallCancelled:       true,
	EventMentionCreate:         true,
	EventApplicationCreate:     true,
	EventApplicationUpdate:     true,
	EventNoticeCreate:          true,
	EventNoticeUpdate:          true,
	EventApplyCreate:           true,
	EventApplyUpdate:           true,
	EventReadStateUpdate:       true,
}
