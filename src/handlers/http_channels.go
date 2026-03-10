package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bubble/src/config"
	"bubble/src/db/models"
	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

// @Summary      Get channel stats
// @Tags         channels
// @Security     BearerAuth
// @Produce      json
// @Param        id   path  int  true  "Channel ID"
// @Success      200  {object}  map[string]any
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/channels/{id}/stats [get]
// getChannelStats 获取频道统计信息
func (h *HTTP) getChannelStats(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(401, gin.H{"error": "未认证"})
		return
	}

	// 解析频道 ID
	channelIDStr := c.Param("id")
	channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的频道ID"})
		return
	}

	// 获取频道信息
	channel, err := h.Svc.GetChannel(uint(channelID))
	if err != nil {
		c.JSON(404, gin.H{"error": "频道不存在"})
		return
	}

	// 检查权限
	isMember, _ := h.Svc.Repo.IsMember(channel.GuildID, u.ID)
	if !isMember {
		c.JSON(403, gin.H{"error": "权限不足"})
		return
	}

	// 获取房间记录
	livekitRoom, err := h.Svc.Repo.GetLiveKitRoomByChannelID(uint(channelID))
	if err != nil || livekitRoom == nil {
		c.JSON(200, gin.H{
			"totalParticipants": 0,
			"totalDuration":     0,
			"isActive":          false,
		})
		return
	}

	// 统计所有参与者
	stats, err := h.Svc.Repo.GetLiveKitRoomStats(livekitRoom.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": "获取统计信息失败"})
		return
	}

	c.JSON(200, stats)
}

// @Summary      LiveKit global stats
// @Tags         livekit
// @Security     BearerAuth
// @Produce      json
// @Success      200  {object}  map[string]any
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Router       /api/livekit/stats [get]
// getLiveKitStats 获取全局 LiveKit 统计（管理员）
func (h *HTTP) getLiveKitStats(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(401, gin.H{"error": "未认证"})
		return
	}

	// 系统管理员检查（ID <= 2 为超级管理员）
	if u.ID > 2 {
		c.JSON(403, gin.H{"error": "需要管理员权限"})
		return
	}

	stats, err := h.Svc.Repo.GetLiveKitGlobalStats()
	if err != nil {
		c.JSON(500, gin.H{"error": "获取统计信息失败"})
		return
	}

	c.JSON(200, stats)
}

// @Summary      Get channel participants
// @Tags         channels
// @Security     BearerAuth
// @Produce      json
// @Param        id   path  int  true  "Channel ID"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/channels/{id}/participants [get]
// getChannelParticipants 获取频道当前参与者列表
func (h *HTTP) getChannelParticipants(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(401, gin.H{"error": "未认证"})
		return
	}

	// 检查 LiveKit 是否配置
	if h.Svc.LiveKit == nil {
		c.JSON(503, gin.H{"error": "音视频服务未配置"})
		return
	}

	// 解析频道 ID
	channelIDStr := c.Param("id")
	channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的频道ID"})
		return
	}

	// 获取频道信息
	channel, err := h.Svc.GetChannel(uint(channelID))
	if err != nil {
		c.JSON(404, gin.H{"error": "频道不存在"})
		return
	}

	// 验证频道类型
	if channel.Type != "media" {
		c.JSON(400, gin.H{"error": "不是媒体频道"})
		return
	}

	// 检查权限
	isMember, _ := h.Svc.Repo.IsMember(channel.GuildID, u.ID)
	if !isMember {
		c.JSON(403, gin.H{"error": "权限不足：非服务器成员"})
		return
	}

	// 获取房间名称
	roomName := fmt.Sprintf("channel_%d", channelID)

	// 从 LiveKit 获取参与者列表
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	participants, err := h.Svc.LiveKit.ListParticipants(ctx, roomName)
	if err != nil {
		// 如果房间不存在，返回空列表
		logger.Warnf("[LiveKit] Failed to list participants for room %s: %v", roomName, err)
		c.JSON(200, gin.H{
			"participants": []interface{}{},
			"count":        0,
		})
		return
	}

	// 格式化参与者信息
	result := make([]map[string]interface{}, 0, len(participants))
	for _, p := range participants {
		// 解析用户 ID
		var userID uint
		fmt.Sscanf(p.Identity, "user_%d", &userID)

		result = append(result, map[string]interface{}{
			"userId":   userID,
			"userName": p.Name,
			"identity": p.Identity,
			"sid":      p.Sid,
			"state":    p.State.String(),
			"joinedAt": time.Unix(p.JoinedAt, 0),
		})
	}

	c.JSON(200, gin.H{
		"participants": result,
		"count":        len(result),
	})
}

// @Summary      Get LiveKit token for channel
// @Description  获取 LiveKit 房间 Token
// @Tags         channels
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Channel ID"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "权限不足"
// @Failure      404  {object}  map[string]string  "频道不存在"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Failure      503  {object}  map[string]string  "音视频服务未配置"
// @Router       /api/channels/{id}/livekit-token [get]
func (h *HTTP) getChannelLiveKitToken(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(401, gin.H{"error": "未认证"})
		return
	}

	// 检查 LiveKit 是否配置
	if h.Svc.LiveKit == nil {
		c.JSON(503, gin.H{"error": "音视频服务未配置"})
		return
	}

	// 解析频道 ID
	channelIDStr := c.Param("id")
	channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的频道ID"})
		return
	}

	// 获取频道信息
	channel, err := h.Svc.GetChannel(uint(channelID))
	if err != nil {
		c.JSON(404, gin.H{"error": "频道不存在"})
		return
	}

	// 验证频道类型
	if channel.Type != "media" {
		c.JSON(400, gin.H{"error": "不是媒体频道"})
		return
	}

	// 检查权限：用户必须是服务器成员且有查看频道权限
	isMember, _ := h.Svc.Repo.IsMember(channel.GuildID, u.ID)
	if !isMember {
		c.JSON(403, gin.H{"error": "权限不足：非服务器成员"})
		return
	}

	hasPerm, err := h.Svc.HasGuildPerm(channel.GuildID, u.ID, service.PermViewChannel)
	if err != nil || !hasPerm {
		c.JSON(403, gin.H{"error": "权限不足"})
		return
	}

	// 确保 LiveKit 房间存在（先在数据库，再在服务器）
	roomName := fmt.Sprintf("channel_%d", channelID)

	// 1. 检查数据库记录
	livekitRoom, err := h.Svc.Repo.GetLiveKitRoomByChannelID(uint(channelID))
	if err != nil || livekitRoom == nil {
		// 数据库中不存在，创建记录
		livekitRoom = &models.LiveKitRoom{
			ChannelID:       uint(channelID),
			RoomName:        roomName,
			IsActive:        true,
			MaxParticipants: 50,
			CreatedAt:       time.Now(),
		}
		if err := h.Svc.Repo.CreateLiveKitRoom(livekitRoom); err != nil {
			logger.Warnf("[LiveKit] Failed to create room record: %v", err)
			c.JSON(500, gin.H{"error": "准备房间失败"})
			return
		}
	}

	// 2. 确保 LiveKit 服务器上房间存在
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	lkRoom, err := h.Svc.LiveKit.GetOrCreateRoom(ctx, channel)
	if err != nil {
		// TODO 出现创建房间失败
		logger.Warnf("[LiveKit] Failed to get or create room %s: %v", roomName, err)
		c.JSON(500, gin.H{"error": "创建房间失败"})
		return
	}

	// 3. 更新数据库中的 RoomSID
	if livekitRoom.RoomSID == "" && lkRoom.Sid != "" {
		if err := h.Svc.Repo.UpdateLiveKitRoom(livekitRoom.ID, map[string]interface{}{
			"room_sid": lkRoom.Sid,
		}); err != nil {
			logger.Warnf("[LiveKit] Failed to update room SID in DB (roomID=%d, sid=%s): %v", livekitRoom.ID, lkRoom.Sid, err)
		}
	}

	// 4. 生成 Token（根据权限决定 canPublish）
	// 默认所有成员都可以发布和订阅，后续可以基于角色权限细化
	canPublish := true
	canSubscribe := true

	token, err := h.Svc.LiveKit.GenerateRoomToken(
		u.ID,
		u.Name,
		roomName,
		canPublish,
		canSubscribe,
	)

	if err != nil {
		logger.Warnf("[LiveKit] Failed to generate token for user %d: %v", u.ID, err)
		c.JSON(500, gin.H{"error": "生成令牌失败"})
		return
	}

	c.JSON(200, gin.H{
		"token":    token,
		"url":      h.Svc.LiveKit.GetURL(),
		"roomName": roomName,
	})
}

// @Summary      Create channel
// @Description  创建频道
// @Tags         channels
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]any  true  "{guildId,name,type,parentId,categoryId}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string  "参数错误"
// @Failure      401   {object}  map[string]string  "未认证"
// @Failure      403   {object}  map[string]string  "权限不足"
// @Router       /api/channels [post]
func (h *HTTP) createChannel(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		GuildID    uint   `json:"guildId"`
		Name       string `json:"name"`
		Type       string `json:"type"` // 频道类型
		ParentID   *uint  `json:"parentId,omitempty"`
		CategoryID *uint  `json:"categoryId,omitempty"` // 添加分类ID支持
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	// 名称长度校验
	n := strings.TrimSpace(body.Name)
	if len([]rune(n)) == 0 || len([]rune(n)) > int(config.MaxChannelNameLength) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "频道名称长度不合法"})
		return
	}
	// permission: MANAGE_CHANNELS
	has, err := h.Svc.HasGuildPerm(body.GuildID, u.ID, service.PermManageChannels)
	if err != nil || !has {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 默认为文本频道
	channelType := body.Type
	if channelType == "" {
		channelType = "text"
	}

	ch, err := h.Svc.CreateChannel(body.GuildID, body.Name, channelType, body.ParentID)
	if err != nil {
		logger.Errorf("[Channels] Failed to create channel in guild %d: %v", body.GuildID, err)
		c.JSON(400, gin.H{"error": "创建频道失败"})
		return
	}
	// 如果提供了categoryID，更新频道分类
	if body.CategoryID != nil {
		_ = h.Svc.SetChannelCategory(ch.ID, *body.CategoryID)
	}

	c.JSON(200, ch)
}

// @Summary      Set channel category
// @Description  设置频道所属分类
// @Tags         channels
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id    path int  true  "Channel ID"
// @Param        body  body object true "{categoryId?: number|null}"
// @Success      200   {object} map[string]bool
// @Failure      400   {object} map[string]string  "参数错误"
// @Failure      401   {object} map[string]string  "未认证"
// @Failure      403   {object} map[string]string  "权限不足"
// @Failure      404   {object} map[string]string  "频道不存在"
// @Router       /api/channels/{id}/category [put]
func (h *HTTP) setChannelCategory(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	channelID64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if channelID64 == 0 {
		c.JSON(400, gin.H{"error": "无效的频道ID"})
		return
	}

	var body struct {
		CategoryID *uint `json:"categoryId"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}

	// 获取频道信息以检查权限
	ch, err := h.Svc.GetChannel(uint(channelID64))
	if err != nil {
		c.JSON(404, gin.H{"error": "频道不存在"})
		return
	}

	// 检查权限
	has, err := h.Svc.HasGuildPerm(ch.GuildID, u.ID, service.PermManageChannels)
	if err != nil || !has {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 设置分类
	if body.CategoryID != nil {
		if err := h.Svc.SetChannelCategory(uint(channelID64), *body.CategoryID); err != nil {
			logger.Errorf("[Channels] Failed to set channel %d category to %d: %v", channelID64, *body.CategoryID, err)
			c.JSON(400, gin.H{"error": "设置分类失败"})
			return
		}
	} else {
		// categoryID为null，清除分类
		if err := h.Svc.ClearChannelCategory(uint(channelID64)); err != nil {
			logger.Errorf("[Channels] Failed to clear channel %d category: %v", channelID64, err)
			c.JSON(400, gin.H{"error": "清除分类失败"})
			return
		}
	}

	c.JSON(200, gin.H{"success": true})
}

// @Summary      Delete channel (soft delete)
// @Description  软删除频道及其子频道（需要 PermManageChannels 权限）
// @Tags         channels
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Channel ID"
// @Success      204  {string}  string  "no content"
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "权限不足"
// @Failure      404  {object}  map[string]string  "频道不存在"
// @Router       /api/channels/{id} [delete]
func (h *HTTP) deleteChannel(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	cid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if cid == 0 {
		c.JSON(400, gin.H{"error": "无效的频道ID"})
		return
	}
	if err := h.Svc.DeleteChannel(uint(cid), u.ID); err != nil {
		if err == service.ErrNotFound {
			// 频道不存在也视为删除成功
			c.Status(204)
			return
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
			return
		} else {
			// logger.Errorf("[Channels] Failed to delete channel %d by user %d: %v", channelID, u.ID, err)
			c.JSON(400, gin.H{"error": "删除频道失败"})
			return
		}
	}
	c.Status(204)
}

// @Summary      List channels
// @Description  列出某服务器的全部频道（用户需具备查看权限）
// @Tags         channels
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        guildId  query  int  true  "Guild ID"
// @Success      200      {array}  map[string]any
// @Failure      400      {object} map[string]string  "参数错误"
// @Failure      401      {object} map[string]string  "未认证"
// @Failure      403      {object} map[string]string  "权限不足"
// @Failure      500      {object} map[string]string  "服务器错误"
// @Router       /api/channels [get]
func (h *HTTP) listChannels(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	gid := parseUintQuery(c, "guildId", 0)
	if gid == 0 {
		c.JSON(400, gin.H{"error": "缺少服务器ID"})
		return
	}

	// Check if user has permission to view channels in this guild
	has, err := h.Svc.HasGuildPerm(gid, u.ID, service.PermViewChannel)
	if err != nil || !has {
		c.JSON(403, gin.H{"error": "权限不足"})
		return
	}

	list, err := h.Svc.ListChannels(gid)
	if err != nil {
		logger.Errorf("[Channels] Failed to list channels for guild %d: %v", gid, err)
		c.JSON(500, gin.H{"error": "获取频道列表失败"})
		return
	}
	c.JSON(200, list)
}

// @Summary      Get structured channels by guild
// @Description  返回结构化的频道数据，包含分类及未分类频道，便于前端直接渲染
// @Tags         channels
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "权限不足"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/guilds/{id}/channels/structured [get]
func (h *HTTP) getGuildChannelStructure(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid64 == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}
	gid := uint(gid64)

	// 权限：查看频道
	ok, err := h.Svc.HasGuildPerm(gid, u.ID, service.PermViewChannel)
	if err != nil || !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 批量获取分类与频道
	cats, err := h.Svc.Repo.ListChannelCategories(gid)
	if err != nil {
		logger.Errorf("[Channels] Failed to list categories for guild %d: %v", gid, err)
		c.JSON(500, gin.H{"error": "获取分类失败"})
		return
	}
	chans, err := h.Svc.ListChannels(gid)
	if err != nil {
		logger.Errorf("[Channels] Failed to list channels for guild %d: %v", gid, err)
		c.JSON(500, gin.H{"error": "获取频道列表失败"})
		return
	}

	// 组装结构：categories[].channels 与 uncategorized[]
	type catOut struct {
		ID        uint             `json:"id"`
		Name      string           `json:"name"`
		SortOrder int              `json:"sortOrder"`
		Channels  []models.Channel `json:"channels"`
	}
	result := struct {
		GuildID       uint             `json:"guildId"`
		Categories    []catOut         `json:"categories"`
		Uncategorized []models.Channel `json:"uncategorized"`
	}{GuildID: gid, Categories: make([]catOut, 0, len(cats)), Uncategorized: make([]models.Channel, 0)}

	// init map categoryID -> index
	idxByCat := make(map[uint]int, len(cats))
	for i, cat := range cats {
		result.Categories = append(result.Categories, catOut{ID: cat.ID, Name: cat.Name, SortOrder: cat.SortOrder})
		idxByCat[cat.ID] = i
	}
	for _, ch := range chans {
		if ch.CategoryID != nil {
			if idx, ok := idxByCat[*ch.CategoryID]; ok {
				result.Categories[idx].Channels = append(result.Categories[idx].Channels, ch)
				continue
			}
		}
		// 未分类或分类已不存在的，归入 uncategorized
		result.Uncategorized = append(result.Uncategorized, ch)
	}

	c.JSON(200, result)
}

// @Summary      List messages
// @Description  获取频道消息列表，支持上下翻页与实时加载
// @Tags         messages
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        channelId  query  int  true   "Channel ID"
// @Param        limit      query  int  false  "Limit (default 50, max 100)"
// @Param        beforeId   query  int  false  "Return messages with id < beforeId (page up)"
// @Param        afterId    query  int  false  "Return messages with id > afterId (page down/live)"
// @Success      200        {array}  map[string]any
// @Failure      400        {object} map[string]string  "参数错误"
// @Failure      401        {object} map[string]string  "未认证"
// @Failure      403        {object} map[string]string  "权限不足"
// @Failure      404        {object} map[string]string  "频道不存在"
// @Failure      500        {object} map[string]string  "服务器错误"
// @Router       /api/messages [get]
func (h *HTTP) getMessages(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	cid := parseUintQuery(c, "channelId", 0)
	if cid == 0 {
		c.JSON(400, gin.H{"error": "缺少频道ID"})
		return
	}

	// Check channel access permission
	ch, err := requireChannelAccess(h.Svc, cid, u.ID, service.PermViewChannel)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "频道不存在"})
		} else {
			c.JSON(403, gin.H{"error": "权限不足"})
		}
		return
	}

	limit, _ := strconv.Atoi(c.Query("limit"))
	// 统一 limit 范围，避免过大开销
	if limit < 1 || limit > 100 {
		limit = 10
	}
	beforeID := parseUintQuery(c, "beforeId", 0)
	afterID := parseUintQuery(c, "afterId", 0)

	msgs, err := h.Svc.GetMessages(ch.ID, limit, beforeID, afterID)
	if err != nil {
		logger.Errorf("[Channels] Failed to get messages for channel %d: %v", ch.ID, err)
		c.JSON(500, gin.H{"error": "获取消息失败"})
		return
	}
	// 判断是否还有更多数据
	hasMore := len(msgs) >= limit
	var nextCursor uint
	if hasMore && len(msgs) > 0 {
		// 向前翻页（beforeId）：nextCursor = 最小的 ID
		// 向后翻页（afterId）：nextCursor = 最大的 ID
		if beforeID > 0 {
			nextCursor = msgs[len(msgs)-1].ID
		} else {
			nextCursor = msgs[len(msgs)-1].ID
		}
	}
	c.JSON(200, gin.H{
		"messages":   msgs,
		"hasMore":    hasMore,
		"nextCursor": nextCursor,
	})
}

// @Summary      List messages with users
// @Description  获取频道消息列表，包含用户数据
// @Tags         messages
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        channelId  query  int  true   "Channel ID"
// @Param        limit      query  int  false  "Limit (default 50, max 100)"
// @Param        beforeId   query  int  false  "Return messages with id < beforeId (page up)"
// @Param        afterId    query  int  false  "Return messages with id > afterId (page down/live)"
// @Success      200        {object}  map[string]any  "{\"messages\": [], \"users\": []}"
// @Failure      400        {object} map[string]string  "参数错误"
// @Failure      401        {object} map[string]string  "未认证"
// @Failure      403        {object} map[string]string  "权限不足"
// @Failure      404        {object} map[string]string  "频道不存在"
// @Failure      500        {object} map[string]string  "服务器错误"
// @Router       /api/messages/with-users [get]
func (h *HTTP) getMessagesWithUsers(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	cid := parseUintQuery(c, "channelId", 0)
	if cid == 0 {
		c.JSON(400, gin.H{"error": "缺少频道ID"})
		return
	}

	// Check channel access permission
	ch, err := requireChannelAccess(h.Svc, cid, u.ID, service.PermViewChannel)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "频道不存在"})
		} else {
			c.JSON(403, gin.H{"error": "权限不足"})
		}
		return
	}

	limit, _ := strconv.Atoi(c.Query("limit"))
	// 统一 limit 范围，避免过大开销
	if limit < 1 || limit > 100 {
		limit = 50
	}
	beforeID := parseUintQuery(c, "beforeId", 0)
	afterID := parseUintQuery(c, "afterId", 0)

	msgs, users, err := h.Svc.GetMessagesWithUsers(ch.ID, limit, beforeID, afterID)
	if err != nil {
		logger.Errorf("[Channels] Failed to get messages with users for channel %d: %v", ch.ID, err)
		c.JSON(500, gin.H{"error": "获取消息失败"})
		return
	}
	// 判断是否还有更多数据
	hasMore := len(msgs) >= limit
	var nextCursor uint
	if hasMore && len(msgs) > 0 {
		// 向前翻页（beforeId）：nextCursor = 最小的 ID
		// 向后翻页（afterId）：nextCursor = 最大的 ID
		if beforeID > 0 {
			nextCursor = msgs[len(msgs)-1].ID
		} else {
			nextCursor = msgs[len(msgs)-1].ID
		}
	}
	c.JSON(200, gin.H{
		"messages":   msgs,
		"users":      users,
		"hasMore":    hasMore,
		"nextCursor": nextCursor,
	})
}

// @Summary      Post message
// @Description  在频道发送消息
// @Tags         messages
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]any  true  "{channelId,content,replyToId,type,platform,fileMeta,tempId,mentions}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string  "参数错误"
// @Failure      401   {object}  map[string]string  "未认证"
// @Failure      403   {object}  map[string]string  "权限不足"
// @Failure      404   {object}  map[string]string  "频道不存在"
// @Router       /api/messages [post]
func (h *HTTP) postMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		ChannelID uint             `json:"channelId"`
		Content   string           `json:"content"`
		ReplyToID *uint            `json:"replyToId"`
		Type      string           `json:"type"`     // optional: text, file, image
		Platform  string           `json:"platform"` // optional: web, mobile, desktop, 默认web
		FileMeta  interface{}      `json:"fileMeta"` // optional: object
		TempID    string           `json:"tempId"`   // optional: 临时消息ID
		Mentions  []map[string]any `json:"mentions"` // optional: 客户端传递完整的 mentions 数据
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	// membership check (channel -> guild)
	ch, err := h.Svc.Repo.GetChannel(body.ChannelID)
	if err != nil {
		c.JSON(404, gin.H{"error": "频道不存在"})
		return
	}
	// permission: SEND_MESSAGES
	has, err := h.Svc.HasGuildPerm(ch.GuildID, u.ID, service.PermSendMessages)
	if err != nil || !has {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	// prepare fileMeta as datatypes.JSON if provided
	var jm datatypes.JSON
	if body.FileMeta != nil {
		h.enrichFileMetaDimensions(c.Request.Context(), body.FileMeta)
		bs, _ := json.Marshal(body.FileMeta)
		jm = datatypes.JSON(bs)
	}
	msgType := "text"
	if strings.TrimSpace(body.Type) != "" {
		msgType = body.Type
	}
	platform := "web"
	if strings.TrimSpace(body.Platform) != "" {
		platform = body.Platform
	}
	// 校验消息长度
	if len([]rune(body.Content)) > int(config.MaxMessageLength) {
		c.JSON(400, gin.H{"error": "消息过长"})
		return
	}
	// 校验 mentions 数量
	if int64(len(body.Mentions)) > config.MaxMessageMentions {
		c.JSON(400, gin.H{"error": "提及人数过多"})
		return
	}
	// 客户端直接传递完整的 mentions 数据，后端只做基本验证和去重
	seenMentions := make(map[string]struct{})
	validMentions := make([]map[string]any, 0, len(body.Mentions))
	for _, mention := range body.Mentions {
		// 提取 type 和 id 作为唯一键
		mType, _ := mention["type"].(string)
		var mID uint
		switch v := mention["id"].(type) {
		case float64:
			mID = uint(v)
		case uint:
			mID = v
		case int:
			mID = uint(v)
		}
		if mType == "" || mID == 0 {
			continue // 跳过无效的 mention
		}
		// 去重
		key := fmt.Sprintf("%s:%d", mType, mID)
		if _, seen := seenMentions[key]; seen {
			continue
		}
		seenMentions[key] = struct{}{}
		validMentions = append(validMentions, mention)
	}
	// 序列化 mentions 为 JSON
	var mentionsJSON datatypes.JSON
	if len(validMentions) > 0 {
		bs, _ := json.Marshal(validMentions)
		mentionsJSON = datatypes.JSON(bs)
	}
	m, err := h.Svc.AddMessage(body.ChannelID, u.ID, u.Name, body.Content, body.ReplyToID, msgType, platform, jm, body.TempID, mentionsJSON)
	if err != nil {
		logger.Errorf("[Channels] Failed to add message to channel %d by user %d: %v", body.ChannelID, u.ID, err)
		c.JSON(404, gin.H{"error": "发送消息失败"})
		return
	}

	// 检测并异步转换音频文件（不阻塞响应）
	if body.FileMeta != nil {
		h.handleAudioConversion(c.Request.Context(), body.FileMeta, u.ID, &ch.GuildID)
	}

	c.JSON(200, m)
	if h.Gw != nil {
		payload := h.buildChannelMessagePayload(m)
		// 直接使用客户端传递的 mentions 数据
		payload["mentions"] = validMentions
		h.Gw.BroadcastToChannel(body.ChannelID, config.EventMessageCreate, payload)

		// 更新红点系统：为频道所有成员（除发送者）增加未读计数
		mentionedUserIDs := make([]uint, 0)
		for _, mr := range validMentions {
			if t, ok := mr["type"].(string); ok && strings.EqualFold(t, "user") {
				var uid uint
				switch v := mr["id"].(type) {
				case float64:
					uid = uint(v)
				case uint:
					uid = v
				case int:
					uid = uint(v)
				}
				if uid > 0 {
					mentionedUserIDs = append(mentionedUserIDs, uid)
				}
			}
		}

		// 异步更新红点计数并广播事件，不阻塞响应
		go func() {
			if err := h.Svc.OnNewChannelMessage(body.ChannelID, m.ID, u.ID, mentionedUserIDs); err != nil {
				logger.Warnf("[ReadState] Failed to update unread counts for channel %d: %v", body.ChannelID, err)
				return
			}
			h.broadcastChannelReadStateUpdates(body.ChannelID, u.ID)
		}()

		// 针对每个被@用户发送 Mention 通知
		for _, mr := range validMentions {
			if t, ok := mr["type"].(string); ok && strings.EqualFold(t, "user") {
				// 尝试从 map 中提取 id
				var uid uint
				switch v := mr["id"].(type) {
				case float64:
					uid = uint(v)
				case uint:
					uid = v
				case int:
					uid = uint(v)
				}
				if uid > 0 {
					// 发送实时 WebSocket 通知（用于弹窗提示）
					h.Gw.BroadcastMention(uid, gin.H{"guildId": ch.GuildID, "channelId": body.ChannelID, "messageId": m.ID, "authorId": u.ID})

					// 创建持久化通知记录（包含公会信息）
					notification, err := h.Svc.CreateMentionNotification(uid, "channel", &ch.GuildID, &body.ChannelID, nil, &m.ID, &u.ID)
					if err != nil {
						logger.Warnf("[Mention] Failed to create notification for user %d: %v", uid, err)
					} else {
						// 获取公会信息
						guild, _ := h.Svc.Repo.GetGuild(ch.GuildID)

						// 构建完整的通知数据（包含关联信息）
						notifPayload := gin.H{
							"id":         notification.ID,
							"userId":     notification.UserID,
							"type":       notification.Type,
							"sourceType": notification.SourceType,
							"guildId":    ch.GuildID,
							"channelId":  body.ChannelID,
							"messageId":  m.ID,
							"authorId":   u.ID,
							"read":       false,
							"createdAt":  notification.CreatedAt,
						}

						// 添加公会信息
						if guild != nil {
							notifPayload["guild"] = gin.H{
								"id":     guild.ID,
								"name":   guild.Name,
								"avatar": guild.Avatar,
							}
						}

						// 添加频道信息
						notifPayload["channel"] = gin.H{
							"id":   ch.ID,
							"name": ch.Name,
							"type": ch.Type,
						}

						// 添加消息内容预览
						notifPayload["message"] = gin.H{
							"id":      m.ID,
							"content": m.Content,
							"type":    m.Type,
						}

						// 添加作者信息
						notifPayload["author"] = gin.H{
							"id":     u.ID,
							"name":   u.Name,
							"avatar": u.Avatar,
						}

						// 发送 NOTICE_CREATE 事件（用于更新通知列表）
						h.Gw.BroadcastNotice(uid, notifPayload)
					}
				}
			}
		}
	}
}

// @Summary      Delete message (recall)
// @Tags         messages
// @Security     BearerAuth
// @Produce      json
// @Param        id   path  int  true  "Message ID"
// @Success      204  {string}  string  "no content"
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/messages/{id} [delete]
// deleteMessage 撤回频道消息（只能撤回自己的消息）。
func (h *HTTP) deleteMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	messageID := uint(id)

	// 获取消息以获取channelID用于广播
	msg, err := h.Svc.Repo.GetMessage(messageID)
	if err != nil {
		// 消息不存在也视为删除成功
		c.Status(204)
		return
	}

	// 撤回消息
	if err := h.Svc.DeleteMessage(messageID, u.ID); err != nil {
		if err == service.ErrNotFound {
			// 消息不存在也视为删除成功
			c.Status(204)
			return
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "只能撤回自己的消息"})
			return
		} else {
			logger.Errorf("[Channels] Failed to delete message %d by user %d: %v", messageID, u.ID, err)
			c.JSON(400, gin.H{"error": "删除消息失败"})
			return
		}
	}

	// 广播撤回事件（统一载荷）
	if h.Gw != nil {
		payload := h.buildChannelMessagePayload(msg)
		// 标识为删除动作，前端可据此处理
		payload["deleted"] = true
		h.Gw.BroadcastToChannel(msg.ChannelID, config.EventMessageDelete, payload)
	}

	c.Status(204)
}

// batchDeleteMessages 批量撤回频道消息
// @Summary      Batch delete channel messages
// @Description  批量撤回频道消息（消息作者、服务器拥有者或具有管理消息权限）
// @Tags         messages
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        channelId  path  int     true  "Channel ID"
// @Param        body       body  object  true  "{messageIds: [uint]}"
// @Success      200        {object}  map[string]any
// @Failure      400        {object}  map[string]string
// @Failure      401        {object}  map[string]string
// @Router       /api/channels/{channelId}/messages/batch-delete [post]
func (h *HTTP) batchDeleteMessages(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	var body struct {
		MessageIDs []uint `json:"messageIds" binding:"required"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if len(body.MessageIDs) == 0 {
		c.JSON(400, gin.H{"error": "messageIds 不能为空"})
		return
	}
	if len(body.MessageIDs) > 100 {
		c.JSON(400, gin.H{"error": "单次最多撤回100条消息"})
		return
	}

	succeeded := make([]uint, 0, len(body.MessageIDs))
	failed := make([]gin.H, 0)

	for _, mid := range body.MessageIDs {
		// 获取消息用于广播
		msg, err := h.Svc.Repo.GetMessage(mid)
		if err != nil {
			// 消息不存在视为成功
			succeeded = append(succeeded, mid)
			continue
		}

		if err := h.Svc.DeleteMessage(mid, u.ID); err != nil {
			reason := "撤回失败"
			if err == service.ErrUnauthorized {
				reason = "权限不足"
			}
			failed = append(failed, gin.H{"messageId": mid, "error": reason})
			continue
		}

		succeeded = append(succeeded, mid)

		// 广播撤回事件
		if h.Gw != nil {
			payload := h.buildChannelMessagePayload(msg)
			payload["deleted"] = true
			h.Gw.BroadcastToChannel(msg.ChannelID, config.EventMessageDelete, payload)
		}
	}

	c.JSON(200, gin.H{
		"succeeded": succeeded,
		"failed":    failed,
	})
}

// @Summary      Update message (edit)
// @Tags         messages
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Message ID"
// @Param        body body  object  true  "{content}"
// @Success      200  {object}  models.Message
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/messages/{id} [put]
// updateMessage 编辑频道消息（只能编辑自己的消息）。
func (h *HTTP) updateMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	messageID := uint(id)
	var body struct {
		Content  string           `json:"content"`
		Mentions []map[string]any `json:"mentions"` // 客户端传递完整的 mentions 数据
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	// 校验消息长度
	if len([]rune(body.Content)) > int(config.MaxMessageLength) {
		c.JSON(400, gin.H{"error": "消息过长"})
		return
	}
	// 读取消息并校验作者
	msg, err := h.Svc.Repo.GetMessage(messageID)
	if err != nil {
		c.JSON(404, gin.H{"error": "消息不存在"})
		return
	}
	if msg.AuthorID != u.ID {
		c.JSON(403, gin.H{"error": "只能编辑自己的消息"})
		return
	}
	// 获取频道信息（用于获取公会ID）
	ch, err := h.Svc.Repo.GetChannel(msg.ChannelID)
	if err != nil {
		c.JSON(404, gin.H{"error": "频道不存在"})
		return
	}
	// 更新内容
	now := time.Now()
	msg.Content = body.Content
	msg.EditedAt = &now
	if err := h.Svc.Repo.UpdateMessage(msg); err != nil {
		logger.Errorf("[Channels] Failed to update message %d: %v", msg.ID, err)
		c.JSON(400, gin.H{"error": "更新消息失败"})
		return
	}
	c.JSON(200, msg)
	// 客户端直接传递完整的 mentions 数据，后端只做基本验证和去重
	var validMentions []map[string]any
	if int64(len(body.Mentions)) > config.MaxMessageMentions {
		// 超过上限则截断到上限
		body.Mentions = body.Mentions[:config.MaxMessageMentions]
	}
	seenMentions := make(map[string]struct{})
	for _, mention := range body.Mentions {
		// 提取 type 和 id 作为唯一键
		mType, _ := mention["type"].(string)
		var mID uint
		switch v := mention["id"].(type) {
		case float64:
			mID = uint(v)
		case uint:
			mID = v
		case int:
			mID = uint(v)
		}
		if mType == "" || mID == 0 {
			continue // 跳过无效的 mention
		}
		// 去重
		key := fmt.Sprintf("%s:%d", mType, mID)
		if _, seen := seenMentions[key]; seen {
			continue
		}
		seenMentions[key] = struct{}{}
		validMentions = append(validMentions, mention)
	}

	// 广播编辑事件（统一载荷并更新 mentions）
	if h.Gw != nil {
		payload := h.buildChannelMessagePayload(msg)
		payload["mentions"] = validMentions
		h.Gw.BroadcastToChannel(msg.ChannelID, config.EventMessageUpdate, payload)
		// 仅对用户类型的 mentions 发送提及通知
		for _, mr := range validMentions {
			if t, ok := mr["type"].(string); ok && strings.EqualFold(t, "user") {
				// 尝试从 map 中提取 id
				var uid uint
				switch v := mr["id"].(type) {
				case float64:
					uid = uint(v)
				case uint:
					uid = v
				case int:
					uid = uint(v)
				}
				if uid > 0 {
					h.Gw.BroadcastMention(uid, gin.H{"guildId": ch.GuildID, "channelId": msg.ChannelID, "messageId": msg.ID, "authorId": u.ID})
				}
			}
		}
	}
}

// buildChannelMessagePayload 统一封装频道消息载荷
func (h *HTTP) buildChannelMessagePayload(msg *models.Message) gin.H {
	payload := gin.H{
		"id":        msg.ID,
		"channelId": msg.ChannelID,
		"authorId":  msg.AuthorID,
		"author":    msg.Author,
		"content":   msg.Content,
		"type":      msg.Type,
		"fileMeta":  msg.FileMeta,
		"replyToId": msg.ReplyToID,
		"createdAt": msg.CreatedAt,
		"createdTs": msg.CreatedAt.UnixMilli(),
	}
	// guildId 通过频道查询补充
	ch, err := h.Svc.Repo.GetChannel(msg.ChannelID)
	if err == nil && ch != nil {
		payload["guildId"] = ch.GuildID

		// 添加频道信息
		channelInfo := gin.H{
			"id":   ch.ID,
			"name": ch.Name,
			"type": ch.Type,
		}
		if ch.ParentID != nil {
			channelInfo["parentId"] = *ch.ParentID
		}
		if ch.CategoryID != nil {
			channelInfo["categoryId"] = *ch.CategoryID
		}
		payload["channelInfo"] = channelInfo

		// 添加公会信息
		if guild, err := h.Svc.Repo.GetGuild(ch.GuildID); err == nil && guild != nil {
			guildInfo := gin.H{
				"id":          guild.ID,
				"name":        guild.Name,
				"avatar":      guild.Avatar,
				"description": guild.Description,
				"ownerId":     guild.OwnerID,
			}
			payload["guildInfo"] = guildInfo
		}

		// 查询作者的完整用户信息和公会成员信息
		if usr, err := h.Svc.GetUserByID(msg.AuthorID); err == nil && usr != nil {
			// 基础用户信息
			authorInfo := gin.H{
				"id":     usr.ID,
				"name":   usr.Name,
				"avatar": usr.Avatar,
				"status": usr.Status,
				"isBot":  usr.IsBot,
			}

			// 查询该用户在公会中的成员信息（昵称、角色等）
			var member models.GuildMember
			if err := h.Svc.Repo.DB.Where("guild_id = ? AND user_id = ?", ch.GuildID, usr.ID).First(&member).Error; err == nil {
				if member.TempNickname != "" {
					authorInfo["nickname"] = member.TempNickname
				}
			}

			// 查询该用户在公会中的角色
			var mrs []models.MemberRole
			if h.Svc.Repo.DB.Where("guild_id = ? AND user_id = ?", ch.GuildID, usr.ID).Find(&mrs).Error == nil && len(mrs) > 0 {
				roleIDs := make([]uint, 0, len(mrs))
				for _, mr := range mrs {
					roleIDs = append(roleIDs, mr.RoleID)
				}
				var roles []models.Role
				if h.Svc.Repo.DB.Where("id IN ?", roleIDs).Find(&roles).Error == nil {
					authorInfo["roles"] = roles
				}
			}

			payload["authorInfo"] = authorInfo
		}
	}
	// tempId 用于前端去重，避免临时消息与真实消息重复显示
	if msg.TempID != "" {
		payload["tempId"] = msg.TempID
	}
	// mentions: 从数据库加载的 JSON 字段
	if len(msg.Mentions) > 0 {
		payload["mentions"] = msg.Mentions
	} else {
		payload["mentions"] = []map[string]any{}
	}
	return payload
}

// @Summary      Reorder channels
// @Tags         channels
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Param        body body  object  true  "Array of {id, sortOrder}"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Router       /api/guilds/{id}/channels/reorder [put]
// reorderChannels 批量更新频道排序
func (h *HTTP) reorderChannels(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}
	var body struct {
		Orders []struct {
			ID        uint `json:"id"`
			SortOrder int  `json:"sortOrder"`
		} `json:"orders"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.ReorderChannels(uint(gid), u.ID, body.Orders); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "频道不存在"})
		} else {
			logger.Errorf("[Channels] Failed to reorder channels in guild %d by user %d: %v", gid, u.ID, err)
			c.JSON(400, gin.H{"error": "重新排序失败"})
		}
		return
	}
	c.JSON(200, gin.H{"success": true})
}

// @Summary      Update channel
// @Description  更新频道信息（名称、横幅等，需要管理频道权限）
// @Tags         channels
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Channel ID"
// @Param        body body  map[string]string true "{name?, banner?}"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Router       /api/channels/{id} [put]
func (h *HTTP) updateChannel(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	channelID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if channelID == 0 {
		c.JSON(400, gin.H{"error": "无效的频道ID"})
		return
	}
	var body struct {
		Name   *string `json:"name,omitempty"`
		Banner *string `json:"banner,omitempty"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.UpdateChannel(uint(channelID), u.ID, body.Name, body.Banner); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "频道不存在"})
		} else {
			logger.Errorf("[Channels] Failed to update channel %d by user %d: %v", channelID, u.ID, err)
			c.JSON(400, gin.H{"error": "更新频道失败"})
		}
		return
	}

	// 返回更新后的信息（如果有横幅，则包含URL）
	response := gin.H{"success": true}
	if body.Banner != nil && h.Svc.MinIO != nil {
		bannerURL := h.Svc.MinIO.GetFileURL(*body.Banner)
		response["data"] = gin.H{
			"banner": gin.H{
				"path": *body.Banner,
				"url":  bannerURL,
			},
		}
	}
	c.JSON(200, response)
}

// ==================== 交互消息（用户 → 机器人隐藏消息） ====================

// @Summary      Send interaction to bot
// @Description  发送交互消息给机器人（存储为隐藏消息，不在普通消息列表中显示，仅机器人可见）
// @Tags         interactions
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]any  true  "{channelId, targetBotId, content, data}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Router       /api/interactions [post]
func (h *HTTP) postInteraction(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	var body struct {
		ChannelID   uint   `json:"channelId" binding:"required"`
		TargetBotID uint   `json:"targetBotId" binding:"required"` // 目标机器人的 botUserId
		Content     string `json:"content"`                        // 文本内容（可选）
		Data        any    `json:"data"`                           // 自定义交互数据（可选，由机器人自行解析）
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: channelId 和 targetBotId 必填"})
		return
	}

	// 校验频道存在
	ch, err := h.Svc.Repo.GetChannel(body.ChannelID)
	if err != nil || ch == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "频道不存在"})
		return
	}

	// 校验发送者是频道所在公会的成员
	_, err = h.Svc.Repo.GetMemberByGuildAndUser(ch.GuildID, u.ID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "你不是该服务器的成员"})
		return
	}

	// 校验目标是机器人用户
	botUser, err := h.Svc.GetUserByID(body.TargetBotID)
	if err != nil || botUser == nil || !botUser.IsBot {
		c.JSON(http.StatusNotFound, gin.H{"error": "目标机器人不存在"})
		return
	}

	// 校验机器人也在该公会中
	_, err = h.Svc.Repo.GetMemberByGuildAndUser(ch.GuildID, body.TargetBotID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "该机器人不在此服务器中"})
		return
	}

	// 将自定义 data 序列化为 embed JSON
	var embedJSON datatypes.JSON
	if body.Data != nil {
		if bs, err := json.Marshal(body.Data); err == nil {
			embedJSON = datatypes.JSON(bs)
		}
	}

	// 存储为隐藏消息（type=interaction），普通消息列表会过滤掉
	msg, err := h.Svc.AddMessage(body.ChannelID, u.ID, u.Name, body.Content, nil, "interaction", "web", embedJSON, "", nil)
	if err != nil {
		logger.Errorf("[Interaction] Failed to save interaction message: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "发送交互消息失败"})
		return
	}

	// 通过 Gateway 以普通 MESSAGE_CREATE 事件定向推送给目标机器人
	if h.Gw != nil {
		payload := h.buildChannelMessagePayload(msg)
		if body.Data != nil {
			payload["data"] = body.Data
		}
		h.Gw.BroadcastToUsers([]uint{body.TargetBotID}, config.EventMessageCreate, payload)
	}

	c.JSON(http.StatusOK, gin.H{"id": msg.ID, "success": true})
}
