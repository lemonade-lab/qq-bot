package handlers

import (
	"net/http"
	"strconv"
	"time"

	"bubble/src/db/models"
	"bubble/src/middleware"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// GuildStatusResponse v2 服务器列表项响应结构
type GuildStatusResponse struct {
	ID           uint      `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Avatar       string    `json:"avatar,omitempty"`
	Banner       string    `json:"banner,omitempty"`
	IsPrivate    bool      `json:"isPrivate"`
	AutoJoinMode string    `json:"autoJoinMode"` // require_approval | no_approval | no_approval_under_100
	Category     string    `json:"category"`     // gaming, work, dev, study, entertainment, other
	Level        int       `json:"level"`        // 服务器等级
	MemberCount  int       `json:"memberCount"`
	CreatedAt    time.Time `json:"createdAt"`
	Status       string    `json:"status"` // "joined" | "pending" | "not_joined"
}

// v2 服务器详情响应结构
func (h *HTTP) GetGuildDetailV2(c *gin.Context) {
	u := middleware.UserFromCtx(c)

	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	guildID := uint(gid64)
	if guildID == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	guild, err := h.Svc.GetGuildDetailV2(guildID, u)
	if err != nil {
		logger.Errorf("[GuildV2] Failed to get guild detail for guild %d: %v", guildID, err)
		c.JSON(500, gin.H{"error": "获取服务器详情失败"})
		return
	}
	if guild == nil {
		c.JSON(404, gin.H{"error": "服务器不存在"})
		return
	}

	Status := "not_joined" // 默认状态：未加入

	// 如果用户已登录，检查加入状态和申请状态
	if u != nil {
		// 检查是否已加入
		isMember, err := h.Svc.IsMember(guild.ID, u.ID)
		if err == nil && isMember {
			Status = "joined"
		}
	}

	// 获取成员数量（从缓存或数据库）
	memberCount := 0
	counts, err := h.Svc.GetGuildMemberCounts([]uint{guild.ID})
	if err == nil {
		if count, ok := counts[guild.ID]; ok {
			memberCount = count
		}
	}

	c.JSON(200, GuildStatusResponse{
		ID:           guild.ID,
		Name:         guild.Name,
		Description:  guild.Description,
		Avatar:       guild.Avatar,
		Banner:       guild.Banner,
		IsPrivate:    guild.IsPrivate,
		AutoJoinMode: guild.AutoJoinMode,
		Category:     guild.Category,
		Level:        guild.Level,
		MemberCount:  memberCount,
		CreatedAt:    guild.CreatedAt,
		Status:       Status, // 使用计算后的状态
	})
}

// @Summary      Search guilds v2 (with join status)
// @Description  模糊搜索服务器列表，支持按分类过滤。附带用户的加入状态。
// @Tags         guilds-v2
// @Security     OptionalAuth
// @Produce      json
// @Param        q        query string false "搜索关键词（名称或ID）"
// @Param        category query string false "按分类过滤: gaming, work, dev, study, entertainment, other"
// @Param        limit    query int    false "Limit (1-20, 默认10)"
// @Success      200  {array} GuildStatusResponse
// @Failure      400  {object} map[string]string
// @Failure      500  {object} map[string]string
// @Router       /api/v2/guilds/search [get]
func (h *HTTP) SearchGuildsV2(c *gin.Context) {
	u := middleware.UserFromCtx(c)

	query := c.Query("q")
	category := c.Query("category")
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 20 {
		limit = 10
	}

	var guilds []models.Guild
	var err error

	if query == "" && category == "" {
		c.JSON(400, gin.H{"error": "请提供搜索关键词(q)或分类(category)"})
		return
	}

	if query != "" {
		// 带关键词搜索（可选分类过滤）
		guilds, err = h.Svc.SearchGuilds(query, limit, category)
	} else {
		// 仅按分类查询（按成员数排序）
		guilds, err = h.Svc.ListGuildsByCategory(category, limit)
	}

	if err != nil {
		logger.Errorf("[GuildV2] Failed to search guilds (q='%s', category='%s'): %v", query, category, err)
		c.JSON(500, gin.H{"error": "搜索服务器失败"})
		return
	}

	// 收集所有 guildID
	guildIDs := make([]uint, len(guilds))
	for i, g := range guilds {
		guildIDs[i] = g.ID
	}

	// 批量检查成员状态
	var membershipMap map[uint]bool
	if u != nil && len(guildIDs) > 0 {
		membershipMap, _ = h.Svc.BatchCheckMembership(u.ID, guildIDs)
	}

	// 批量获取成员数量
	var memberCountMap map[uint]int
	if len(guildIDs) > 0 {
		memberCountMap, _ = h.Svc.GetGuildMemberCounts(guildIDs)
	}

	// 构建响应
	response := make([]GuildStatusResponse, 0, len(guilds))
	for _, guild := range guilds {
		status := "not_joined"
		if u != nil && membershipMap[guild.ID] {
			status = "joined"
		}

		response = append(response, GuildStatusResponse{
			ID:           guild.ID,
			Name:         guild.Name,
			Description:  guild.Description,
			Avatar:       guild.Avatar,
			Banner:       guild.Banner,
			IsPrivate:    guild.IsPrivate,
			AutoJoinMode: guild.AutoJoinMode,
			Category:     guild.Category,
			Level:        guild.Level,
			MemberCount:  memberCountMap[guild.ID],
			CreatedAt:    guild.CreatedAt,
			Status:       status,
		})
	}

	c.JSON(200, response)
}

// @Summary      List hot guilds v2 (with join status)
// @Description  返回热门服务器列表，附带用户的加入状态。支持匿名访问（无需登录）。可按分类过滤。
// @Tags         guilds-v2
// @Security     OptionalAuth
// @Produce      json
// @Param        limit    query int    false "Limit (1-20, 默认10)"
// @Param        category query string false "按分类过滤: gaming, work, dev, study, entertainment, other"
// @Success      200  {array} GuildStatusResponse
// @Failure      500  {object} map[string]string
// @Router       /api/v2/guilds/hot [get]
func (h *HTTP) hotGuildsV2(c *gin.Context) {
	u := middleware.UserFromCtx(c)

	category := c.Query("category")
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 20 {
		limit = 10
	}

	var guilds []models.Guild
	var err error

	if category != "" {
		// 按分类查询热门服务器
		guilds, err = h.Svc.ListGuildsByCategory(category, limit)
	} else {
		// 全局热门服务器
		guilds, err = h.Svc.HotGuilds(limit)
	}
	if err != nil {
		logger.Errorf("[GuildV2] Failed to get hot guilds (category='%s'): %v", category, err)
		c.JSON(500, gin.H{"error": "获取热门服务器失败"})
		return
	}

	// 收集所有 guildID
	guildIDs := make([]uint, len(guilds))
	for i, g := range guilds {
		guildIDs[i] = g.ID
	}

	// 批量检查成员状态
	var membershipMap map[uint]bool
	if u != nil && len(guildIDs) > 0 {
		membershipMap, _ = h.Svc.BatchCheckMembership(u.ID, guildIDs)
	}

	// 批量获取成员数量
	var memberCountMap map[uint]int
	if len(guildIDs) > 0 {
		memberCountMap, _ = h.Svc.GetGuildMemberCounts(guildIDs)
	}

	// 构建响应
	response := make([]GuildStatusResponse, 0, len(guilds))
	for _, guild := range guilds {
		status := "not_joined"
		if u != nil && membershipMap[guild.ID] {
			status = "joined"
		}

		response = append(response, GuildStatusResponse{
			ID:           guild.ID,
			Name:         guild.Name,
			Description:  guild.Description,
			Avatar:       guild.Avatar,
			Banner:       guild.Banner,
			IsPrivate:    guild.IsPrivate,
			AutoJoinMode: guild.AutoJoinMode,
			Category:     guild.Category,
			Level:        guild.Level,
			MemberCount:  memberCountMap[guild.ID],
			CreatedAt:    guild.CreatedAt,
			Status:       status,
		})
	}

	c.JSON(200, response)
}

// @Summary      Apply to join guild v2
// @Description  申请加入服务器。用户可以无限次点击申请，统一返回"已发送申请"。
//   - 超过1天的pending申请视为过期，会自动创建新的申请记录
//   - 已处理的申请（approved/rejected）不会阻止创建新申请
//   - 不会告知用户服务器是否处理了申请
//
// @Tags         guilds-v2
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Param        body body  object  false  "{note: '申请理由'}"
// @Success      200  {object}  map[string]string  "{"status": "applied", "message": "已发送申请"}"
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Router       /api/v2/guilds/{id}/apply [post]
func (h *HTTP) applyToJoinGuildV2(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	guildID := uint(gid64)
	if guildID == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	// 检查服务器是否存在
	guild, err := h.Svc.Repo.GetGuild(guildID)
	if err != nil || guild == nil {
		c.JSON(404, gin.H{"error": "服务器不存在"})
		return
	}

	// 检查用户是否已经是成员
	isMember, err := h.Svc.IsMember(guildID, u.ID)
	if err != nil {
		logger.Errorf("[GuildV2] Failed to check membership for guild %d user %d: %v", guildID, u.ID, err)
		c.JSON(500, gin.H{"error": "查询成员状态失败"})
		return
	}
	if isMember {
		c.JSON(200, gin.H{
			"status":  "join",
			"message": "已加入",
		})
		return
	}

	// 解析申请备注（可选）
	var body struct {
		Note string `json:"note"`
	}
	// 使用 ShouldBindJSON 因为备注是可选的
	_ = c.ShouldBindJSON(&body)

	// 处理申请逻辑
	guild, notif, err := h.Svc.ProcessGuildApplicationV2(guildID, u.ID, body.Note)
	if err != nil {
		logger.Errorf("[GuildV2] Failed to process guild application for guild %d user %d: %v", guildID, u.ID, err)
		c.JSON(500, gin.H{"error": "处理申请失败"})
		return
	}

	// 如果因 autoJoinMode 直接加入（或已是成员），返回 joined
	if guild != nil {
		if ok, _ := h.Svc.IsMember(guildID, u.ID); ok {
			c.JSON(200, gin.H{
				"status":  "join",
				"message": "已加入",
			})
			return
		}
	}

	// 如果创建了新申请并且有通知，推送通知给服务器所有者
	if notif != nil && guild != nil && h.Gw != nil {
		notifPayload := gin.H{
			"id":         notif.ID,
			"userId":     notif.UserID,
			"type":       notif.Type,
			"sourceType": notif.SourceType,
			"status":     "pending",
			"read":       false,
			"createdAt":  notif.CreatedAt,
		}

		// 添加服务器信息
		if notif.GuildID != nil {
			notifPayload["guildId"] = *notif.GuildID
			notifPayload["guild"] = gin.H{
				"id":     guild.ID,
				"name":   guild.Name,
				"avatar": guild.Avatar,
			}
		}

		// 添加申请人信息
		if notif.AuthorID != nil {
			notifPayload["authorId"] = *notif.AuthorID
			notifPayload["author"] = gin.H{
				"id":     u.ID,
				"name":   u.Name,
				"avatar": u.Avatar,
			}
		}

		h.Gw.BroadcastNotice(guild.OwnerID, notifPayload)
	}

	// 统一返回"已发送申请"
	c.JSON(200, gin.H{
		"status":  "applied",
		"message": "已发送申请",
	})
}
