package handlers

import (
	"encoding/json"

	"bubble/src/config"
	"bubble/src/db/models"
	"bubble/src/logger"
)

// LiveKit 相关的 Gateway 扩展
// Op codes 定义在 gateway.go 中

// channelVoiceStatePayload 频道语音状态
// 注意：前端期望 state 对象，所以我们将状态字段包装在 state 中
type channelVoiceStatePayload struct {
	ChannelID uint                     `json:"channelId"`
	UserID    uint                     `json:"userId"`
	UserName  string                   `json:"userName"`
	Action    string                   `json:"action"` // joined | left | muted | unmuted | video_on | video_off | screenshare_on | screenshare_off
	State     channelVoiceStateDetails `json:"state,omitempty"`
}

// channelVoiceStateDetails 状态详情
type channelVoiceStateDetails struct {
	IsMuted        *bool `json:"isMuted,omitempty"`
	HasVideo       *bool `json:"hasVideo,omitempty"`
	HasScreenShare *bool `json:"hasScreenShare,omitempty"`
}

// BroadcastChannelVoiceState 广播频道语音状态变化
// channelID: 频道 ID
// userID: 用户 ID
// userName: 用户名
// action: 动作类型
// state: 额外状态信息
func (g *Gateway) BroadcastChannelVoiceState(channelID, userID uint, userName, action string, state map[string]interface{}) {
	payload := channelVoiceStatePayload{
		ChannelID: channelID,
		UserID:    userID,
		UserName:  userName,
		Action:    action,
		State:     channelVoiceStateDetails{},
	}

	// 从 state 中提取状态信息，包装在 State 对象中
	if isMuted, ok := state["isMuted"].(bool); ok {
		payload.State.IsMuted = &isMuted
	}
	if hasVideo, ok := state["hasVideo"].(bool); ok {
		payload.State.HasVideo = &hasVideo
	}
	if hasScreenShare, ok := state["hasScreenShare"].(bool); ok {
		payload.State.HasScreenShare = &hasScreenShare
	}

	// 如果 State 对象为空，则不发送（omitempty）
	if payload.State.IsMuted == nil && payload.State.HasVideo == nil && payload.State.HasScreenShare == nil {
		payload.State = channelVoiceStateDetails{}
	}

	// 获取频道所属的服务器
	channel, err := g.svc.GetChannel(channelID)
	if err != nil {
		logger.Errorf("[Gateway] Failed to get channel %d: %v", channelID, err)
		return
	}

	// 获取服务器所有成员（最多1000个）
	members, _, err := g.svc.ListMembers(channel.GuildID, 1, 1000)
	if err != nil {
		logger.Errorf("[Gateway] Failed to get guild members: %v", err)
		return
	}

	// 提取用户 ID 列表
	userIDs := make([]uint, 0, len(members))
	for _, member := range members {
		userIDs = append(userIDs, member.UserID)
	}

	// 广播给所有服务器成员
	g.BroadcastToUsers(userIDs, config.EventChannelVoiceState, payload)
	logger.Debugf("[Gateway] Broadcasted channel voice state: channel=%d, user=%d, action=%s", channelID, userID, action)
}

// dmVoiceStatePayload 私聊语音状态
type dmVoiceStatePayload struct {
	ThreadID uint                     `json:"threadId"`
	UserID   uint                     `json:"userId"`
	UserName string                   `json:"userName"`
	Action   string                   `json:"action"`
	State    channelVoiceStateDetails `json:"state,omitempty"`
}

// BroadcastDmVoiceState 广播私聊语音状态变化到该线程的订阅者
func (g *Gateway) BroadcastDmVoiceState(threadID, userID uint, userName, action string, state map[string]interface{}) {
	payload := dmVoiceStatePayload{
		ThreadID: threadID,
		UserID:   userID,
		UserName: userName,
		Action:   action,
		State:    channelVoiceStateDetails{},
	}

	if isMuted, ok := state["isMuted"].(bool); ok {
		payload.State.IsMuted = &isMuted
	}
	if hasVideo, ok := state["hasVideo"].(bool); ok {
		payload.State.HasVideo = &hasVideo
	}
	if hasScreenShare, ok := state["hasScreenShare"].(bool); ok {
		payload.State.HasScreenShare = &hasScreenShare
	}

	if payload.State.IsMuted == nil && payload.State.HasVideo == nil && payload.State.HasScreenShare == nil {
		payload.State = channelVoiceStateDetails{}
	}

	// 广播到该 DM 线程订阅者 — 获取参与者并传入以避免 Gateway 做 DB 查
	if th, err := g.svc.Repo.GetDmThread(threadID); err == nil && th != nil {
		g.BroadcastToDM(threadID, config.EventDmVoiceState, payload, []uint{th.UserAID, th.UserBID})
	} else {
		g.BroadcastToDM(threadID, config.EventDmVoiceState, payload, nil)
	}
	logger.Infof("[Gateway] Broadcasted DM voice state: thread=%d, user=%d, action=%s", threadID, userID, action)
}

// handleChannelVoiceJoin 处理用户加入语音频道
func (g *Gateway) handleChannelVoiceJoin(gc *gwConn, payload json.RawMessage) {
	var p struct {
		ChannelID uint `json:"channelId"`
	}

	if err := json.Unmarshal(payload, &p); err != nil {
		logger.Warnf("[Gateway] Failed to unmarshal channel voice join: %v", err)
		return
	}

	logger.Infof("[Gateway] User %d joining voice channel %d", gc.userID, p.ChannelID)

	// 获取用户信息
	user, err := g.svc.GetUserByID(gc.userID)
	if err != nil {
		logger.Errorf("[Gateway] Failed to get user: %v", err)
		return
	}

	// 广播加入事件
	g.BroadcastChannelVoiceState(p.ChannelID, gc.userID, user.Name, "joined", map[string]interface{}{
		"isMuted":  false, // 默认未静音
		"hasVideo": false, // 默认关闭视频
	})
}

// handleChannelVoiceLeave 处理用户离开语音频道
func (g *Gateway) handleChannelVoiceLeave(gc *gwConn, payload json.RawMessage) {
	var p struct {
		ChannelID uint `json:"channelId"`
	}

	if err := json.Unmarshal(payload, &p); err != nil {
		logger.Warnf("[Gateway] Failed to unmarshal channel voice leave: %v", err)
		return
	}

	logger.Infof("[Gateway] User %d leaving voice channel %d", gc.userID, p.ChannelID)

	// 获取用户信息
	user, err := g.svc.GetUserByID(gc.userID)
	if err != nil {
		logger.Errorf("[Gateway] Failed to get user: %v", err)
		return
	}

	// 广播离开事件
	g.BroadcastChannelVoiceState(p.ChannelID, gc.userID, user.Name, "left", map[string]interface{}{})
}

// handleChannelVoiceUpdate 处理语音状态更新
func (g *Gateway) handleChannelVoiceUpdate(gc *gwConn, payload json.RawMessage) {
	var p struct {
		ChannelID      uint  `json:"channelId"`
		IsMuted        *bool `json:"isMuted,omitempty"`
		HasVideo       *bool `json:"hasVideo,omitempty"`
		HasScreenShare *bool `json:"hasScreenShare,omitempty"`
	}

	if err := json.Unmarshal(payload, &p); err != nil {
		logger.Warnf("[Gateway] Failed to unmarshal channel voice update: %v", err)
		return
	}

	logger.Infof("[Gateway] User %d updating voice state in channel %d", gc.userID, p.ChannelID)

	// 获取用户信息
	user, err := g.svc.GetUserByID(gc.userID)
	if err != nil {
		logger.Errorf("[Gateway] Failed to get user: %v", err)
		return
	}

	// 确定动作类型
	action := "updated"
	state := make(map[string]interface{})

	if p.IsMuted != nil {
		state["isMuted"] = *p.IsMuted
		if *p.IsMuted {
			action = "muted"
		} else {
			action = "unmuted"
		}
	}

	if p.HasVideo != nil {
		state["hasVideo"] = *p.HasVideo
		if *p.HasVideo {
			action = "video_on"
		} else {
			action = "video_off"
		}
	}

	if p.HasScreenShare != nil {
		state["hasScreenShare"] = *p.HasScreenShare
		if *p.HasScreenShare {
			action = "screenshare_on"
		} else {
			action = "screenshare_off"
		}
	}

	// 广播状态更新
	g.BroadcastChannelVoiceState(p.ChannelID, gc.userID, user.Name, action, state)
}

// 在 Gateway.handleMessage 中添加新的消息处理
// 您需要在 gateway.go 的 handleMessage 函数中添加以下 case:
/*
case config.OpChannelVoiceJoin:
	g.handleChannelVoiceJoin(gc, frame.D)
case config.OpChannelVoiceLeave:
	g.handleChannelVoiceLeave(gc, frame.D)
case config.OpChannelVoiceUpdate:
	g.handleChannelVoiceUpdate(gc, frame.D)
case config.OpDmCallStart:
	g.handleDmCallStart(gc, frame.D)
*/

// OpDmCallStart 私聊发起通话
// Op codes centralized in config

// dmCallIncomingPayload 私聊来电通知
type dmCallIncomingPayload struct {
	FromUserID   uint   `json:"fromUserId"`
	FromUserName string `json:"fromUserName"`
	ThreadID     uint   `json:"threadId"`
	IsVideo      bool   `json:"isVideo"`
}

// handleDmCallStart 处理私聊发起通话
func (g *Gateway) handleDmCallStart(gc *gwConn, payload json.RawMessage) {
	var p struct {
		ThreadID uint `json:"threadId"`
		IsVideo  bool `json:"isVideo"`
	}

	if err := json.Unmarshal(payload, &p); err != nil {
		logger.Warnf("[Gateway] Failed to unmarshal DM call start: %v", err)
		return
	}

	logger.Infof("[Gateway] User %d starting DM call in thread %d (video: %v)", gc.userID, p.ThreadID, p.IsVideo)

	// 获取线程信息
	var thread models.DmThread
	if err := g.svc.Repo.DB.First(&thread, p.ThreadID).Error; err != nil {
		logger.Errorf("[Gateway] Failed to get DM thread: %v", err)
		return
	}

	// 确认发起者是线程参与者
	if !(thread.UserAID == gc.userID || thread.UserBID == gc.userID) {
		logger.Warnf("[Gateway] User %d is not a participant of thread %d", gc.userID, p.ThreadID)
		return
	}

	// 获取发起者信息
	caller, err := g.svc.GetUserByID(gc.userID)
	if err != nil {
		logger.Errorf("[Gateway] Failed to get caller user: %v", err)
		return
	}

	// 确定接收者ID
	receiverID := thread.UserAID
	if receiverID == gc.userID {
		receiverID = thread.UserBID
	}

	// 发送来电通知给接收者
	notification := dmCallIncomingPayload{
		FromUserID:   gc.userID,
		FromUserName: caller.Name,
		ThreadID:     p.ThreadID,
		IsVideo:      p.IsVideo,
	}

	g.BroadcastToUsers([]uint{receiverID}, config.EventDmCallIncoming, notification)
	logger.Debugf("[Gateway] Sent DM call notification to user %d from %s (thread: %d)", receiverID, caller.Name, p.ThreadID)
}

// dmCallAcceptedPayload 接听通话响应
type dmCallAcceptedPayload struct {
	ThreadID uint `json:"threadId"`
	UserID   uint `json:"userId"`
}

// handleDmCallAccept 处理私聊接听通话
func (g *Gateway) handleDmCallAccept(gc *gwConn, payload json.RawMessage) {
	var p struct {
		ThreadID uint `json:"threadId"`
	}

	if err := json.Unmarshal(payload, &p); err != nil {
		logger.Warnf("[Gateway] Failed to unmarshal DM call accept: %v", err)
		return
	}

	logger.Infof("[Gateway] User %d accepting DM call in thread %d", gc.userID, p.ThreadID)

	// 获取线程信息
	var thread models.DmThread
	if err := g.svc.Repo.DB.First(&thread, p.ThreadID).Error; err != nil {
		logger.Errorf("[Gateway] Failed to get DM thread: %v", err)
		return
	}

	// 确认接听者是线程参与者
	if !(thread.UserAID == gc.userID || thread.UserBID == gc.userID) {
		logger.Warnf("[Gateway] User %d is not a participant of thread %d", gc.userID, p.ThreadID)
		return
	}

	// 确定发起者ID（另一个参与者）
	callerID := thread.UserAID
	if callerID == gc.userID {
		callerID = thread.UserBID
	}

	// 发送接听通知给发起者
	notification := dmCallAcceptedPayload{
		ThreadID: p.ThreadID,
		UserID:   gc.userID,
	}

	g.BroadcastToUsers([]uint{callerID}, config.EventDmCallAccepted, notification)
	logger.Debugf("[Gateway] Sent DM call accepted notification to user %d (thread: %d)", callerID, p.ThreadID)
}

// dmCallRejectedPayload 拒绝通话响应
type dmCallRejectedPayload struct {
	ThreadID uint `json:"threadId"`
	UserID   uint `json:"userId"`
}

// handleDmCallReject 处理私聊拒绝通话
func (g *Gateway) handleDmCallReject(gc *gwConn, payload json.RawMessage) {
	var p struct {
		ThreadID uint `json:"threadId"`
	}

	if err := json.Unmarshal(payload, &p); err != nil {
		logger.Warnf("[Gateway] Failed to unmarshal DM call reject: %v", err)
		return
	}

	logger.Infof("[Gateway] User %d rejecting DM call in thread %d", gc.userID, p.ThreadID)

	// 获取线程信息
	var thread models.DmThread
	if err := g.svc.Repo.DB.First(&thread, p.ThreadID).Error; err != nil {
		logger.Errorf("[Gateway] Failed to get DM thread: %v", err)
		return
	}

	// 确认拒绝者是线程参与者
	if !(thread.UserAID == gc.userID || thread.UserBID == gc.userID) {
		logger.Warnf("[Gateway] User %d is not a participant of thread %d", gc.userID, p.ThreadID)
		return
	}

	// 确定发起者ID
	callerID := thread.UserAID
	if callerID == gc.userID {
		callerID = thread.UserBID
	}

	// 发送拒绝通知给发起者
	notification := dmCallRejectedPayload{
		ThreadID: p.ThreadID,
		UserID:   gc.userID,
	}

	g.BroadcastToUsers([]uint{callerID}, config.EventDmCallRejected, notification)
	logger.Debugf("[Gateway] Sent DM call rejected notification to user %d (thread: %d)", callerID, p.ThreadID)
}

// dmCallCancelledPayload 取消通话响应
type dmCallCancelledPayload struct {
	ThreadID uint `json:"threadId"`
	UserID   uint `json:"userId"`
}

// handleDmCallCancel 处理私聊取消通话
func (g *Gateway) handleDmCallCancel(gc *gwConn, payload json.RawMessage) {
	var p struct {
		ThreadID uint `json:"threadId"`
	}

	if err := json.Unmarshal(payload, &p); err != nil {
		logger.Warnf("[Gateway] Failed to unmarshal DM call cancel: %v", err)
		return
	}

	logger.Infof("[Gateway] User %d cancelling DM call in thread %d", gc.userID, p.ThreadID)

	// 获取线程信息
	var thread models.DmThread
	if err := g.svc.Repo.DB.First(&thread, p.ThreadID).Error; err != nil {
		logger.Errorf("[Gateway] Failed to get DM thread: %v", err)
		return
	}

	// 确认取消者是线程参与者
	if !(thread.UserAID == gc.userID || thread.UserBID == gc.userID) {
		logger.Warnf("[Gateway] User %d is not a participant of thread %d", gc.userID, p.ThreadID)
		return
	}

	// 确定接收者ID
	receiverID := thread.UserAID
	if receiverID == gc.userID {
		receiverID = thread.UserBID
	}

	// 发送取消通知给接收者
	notification := dmCallCancelledPayload{
		ThreadID: p.ThreadID,
		UserID:   gc.userID,
	}

	g.BroadcastToUsers([]uint{receiverID}, config.EventDmCallCancelled, notification)
	logger.Debugf("[Gateway] Sent DM call cancelled notification to user %d (thread: %d)", receiverID, p.ThreadID)
}
