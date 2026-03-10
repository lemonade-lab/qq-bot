package handlers

import (
	"net/http"
	"strconv"

	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      Get guild media (images)
// @Description  获取服务器相册（图片列表），仅限服务器成员可查看
// @Tags         guilds
// @Security     BearerAuth
// @Produce      json
// @Param        id     path   int    true   "Guild ID"
// @Param        limit  query  int    false  "每页数量(默认50，最大100)"
// @Param        before query  int    false  "游标：返回ID小于此值的消息"
// @Success      200    {object}  map[string]any
// @Failure      401    {object}  map[string]string
// @Failure      403    {object}  map[string]string
// @Failure      404    {object}  map[string]string
// @Router       /api/guilds/{id}/media/images [get]
func (h *HTTP) getGuildImages(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	guildID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if guildID == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	// 检查是否为服务器成员
	isMember, _ := h.Svc.Repo.IsMember(uint(guildID), u.ID)
	if !isMember {
		c.JSON(403, gin.H{"error": "仅服务器成员可查看"})
		return
	}

	// 解析分页参数
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	before, _ := strconv.ParseUint(c.Query("before"), 10, 64)

	messages, err := h.Svc.GetGuildMediaByType(uint(guildID), "image", limit, uint(before))
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "服务器不存在"})
		} else {
			logger.Errorf("[GuildMedia] Failed to get images for guild %d: %v", guildID, err)
			c.JSON(400, gin.H{"error": "获取图片列表失败"})
		}
		return
	}

	// 构建响应，包含分页信息
	response := gin.H{
		"data": messages,
		"pagination": gin.H{
			"limit": limit,
		},
	}
	// 如果有数据，返回下一页的游标
	if len(messages) > 0 {
		response["pagination"].(gin.H)["nextCursor"] = messages[len(messages)-1].ID
	}

	c.JSON(200, response)
}

// @Summary      Get guild videos
// @Description  获取频道视频列表，仅限服务器成员可查看
// @Tags         guilds
// @Security     BearerAuth
// @Produce      json
// @Param        id     path   int    true   "Guild ID"
// @Param        limit  query  int    false  "每页数量(默认50，最大100)"
// @Param        before query  int    false  "游标：返回ID小于此值的消息"
// @Success      200    {object}  map[string]any
// @Failure      401    {object}  map[string]string
// @Failure      403    {object}  map[string]string
// @Failure      404    {object}  map[string]string
// @Router       /api/guilds/{id}/media/videos [get]
func (h *HTTP) getGuildVideos(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	guildID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if guildID == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	// 检查是否为服务器成员
	isMember, _ := h.Svc.Repo.IsMember(uint(guildID), u.ID)
	if !isMember {
		c.JSON(403, gin.H{"error": "仅服务器成员可查看"})
		return
	}

	// 解析分页参数
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	before, _ := strconv.ParseUint(c.Query("before"), 10, 64)

	messages, err := h.Svc.GetGuildMediaByType(uint(guildID), "video", limit, uint(before))
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "服务器不存在"})
		} else {
			logger.Errorf("[GuildMedia] Failed to get videos for guild %d: %v", guildID, err)
			c.JSON(400, gin.H{"error": "获取视频列表失败"})
		}
		return
	}

	// 构建响应，包含分页信息
	response := gin.H{
		"data": messages,
		"pagination": gin.H{
			"limit": limit,
		},
	}
	// 如果有数据，返回下一页的游标
	if len(messages) > 0 {
		response["pagination"].(gin.H)["nextCursor"] = messages[len(messages)-1].ID
	}

	c.JSON(200, response)
}

// @Summary      Get guild files
// @Description  获取服务器文件列表，仅限服务器成员可查看
// @Tags         guilds
// @Security     BearerAuth
// @Produce      json
// @Param        id     path   int    true   "Guild ID"
// @Param        limit  query  int    false  "每页数量(默认50，最大100)"
// @Param        before query  int    false  "游标：返回ID小于此值的消息"
// @Success      200    {object}  map[string]any
// @Failure      401    {object}  map[string]string
// @Failure      403    {object}  map[string]string
// @Failure      404    {object}  map[string]string
// @Router       /api/guilds/{id}/media/files [get]
func (h *HTTP) getGuildFiles(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	guildID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if guildID == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	// 检查是否为服务器成员
	isMember, _ := h.Svc.Repo.IsMember(uint(guildID), u.ID)
	if !isMember {
		c.JSON(403, gin.H{"error": "仅服务器成员可查看"})
		return
	}

	// 解析分页参数
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	before, _ := strconv.ParseUint(c.Query("before"), 10, 64)

	messages, err := h.Svc.GetGuildMediaByType(uint(guildID), "file", limit, uint(before))
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "服务器不存在"})
		} else {
			logger.Errorf("[GuildMedia] Failed to get files for guild %d: %v", guildID, err)
			c.JSON(400, gin.H{"error": "获取文件列表失败"})
		}
		return
	}

	// 构建响应，包含分页信息
	response := gin.H{
		"data": messages,
		"pagination": gin.H{
			"limit": limit,
		},
	}
	// 如果有数据，返回下一页的游标
	if len(messages) > 0 {
		response["pagination"].(gin.H)["nextCursor"] = messages[len(messages)-1].ID
	}

	c.JSON(200, response)
}

// @Summary      Delete guild media (owner only)
// @Description  批量删除服务器媒体文件（软删除消息），仅服务器 owner 可操作
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Param        body body  map[string]any true "{messageIds: [1,2,3]}"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/guilds/{id}/media [delete]
func (h *HTTP) deleteGuildMedia(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	guildID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if guildID == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	var body struct {
		MessageIDs []uint `json:"messageIds"`
	}
	if err := c.BindJSON(&body); err != nil || len(body.MessageIDs) == 0 {
		c.JSON(400, gin.H{"error": "请求体格式错误或messageIds为空"})
		return
	}

	// 限制批量删除数量
	if len(body.MessageIDs) > 100 {
		c.JSON(400, gin.H{"error": "数量过多（每次最多100条）"})
		return
	}

	deleted, err := h.Svc.DeleteGuildMedia(uint(guildID), u.ID, body.MessageIDs)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "服务器不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "仅服务器群主可删除媒体"})
		} else {
			logger.Errorf("[GuildMedia] Failed to delete guild media in guild %d: %v", guildID, err)
			c.JSON(400, gin.H{"error": "删除失败"})
		}
		return
	}

	c.JSON(200, gin.H{
		"deleted": deleted,
		"message": "删除成功",
	})
}
