package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"bubble/src/config"
	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

// @Summary      List announcements
// @Tags         announcements
// @Security     BearerAuth
// @Produce      json
// @Param        id   path      int  true  "Guild ID"
// @Success      200   {array}   models.Announcement
// @Failure      401   {object}  map[string]string
// @Router       /api/guilds/{id}/announcements [get]
// listAnnouncements 列出服务器公告（分页）
func (h *HTTP) listAnnouncements(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid, err := parseUintParam(c, "id")
	if err != nil {
		logger.Errorf("[Announcements] Invalid guild ID in listAnnouncements: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	// Cursor-first support
	beforeID64, _ := strconv.ParseUint(c.Query("beforeId"), 10, 64)
	afterID64, _ := strconv.ParseUint(c.Query("afterId"), 10, 64)
	beforeID := uint(beforeID64)
	afterID := uint(afterID64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit < 1 || limit > 100 {
		limit = 10
	}
	if beforeID > 0 || afterID > 0 {
		list, err := h.Svc.ListAnnouncementsCursor(gid, limit, beforeID, afterID)
		if err != nil {
			logger.Errorf("[Announcements] Failed to list announcements by cursor for guild %d: %v", gid, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取公告列表失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": list})
		return
	}
	// Fallback to page-based
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	list, total, err := h.Svc.ListAnnouncements(gid, page, limit)
	if err != nil {
		logger.Errorf("[Announcements] Failed to list announcements for guild %d: %v", gid, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取公告列表失败"})
		return
	}
	hasMore := page*limit < total
	c.JSON(http.StatusOK, gin.H{
		"data": list,
		"pagination": gin.H{
			"page":    page,
			"limit":   limit,
			"total":   total,
			"hasMore": hasMore,
		},
	})
}

// @Summary      Create announcement
// @Tags         announcements
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "Guild ID"
// @Param        body  body      map[string]string  true  "{title,content}"
// @Success      200   {object}  models.Announcement
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Router       /api/guilds/{id}/announcements [post]
// createAnnouncement 创建公告
func (h *HTTP) createAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid, err := parseUintParam(c, "id")
	if err != nil {
		logger.Errorf("[Announcements] Invalid guild ID: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	var body struct {
		Title   string           `json:"title"`
		Content string           `json:"content"`
		Images  []map[string]any `json:"images"` // [{path, url}], 最多9张
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if len([]rune(body.Content)) > int(config.MaxGuildAnnouncementLength) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "公告内容过长"})
		return
	}
	if int64(len(body.Images)) > config.MaxAnnouncementImages {
		c.JSON(http.StatusBadRequest, gin.H{"error": "图片数量超过限制(最多9张)"})
		return
	}
	var imagesJSON datatypes.JSON
	if len(body.Images) > 0 {
		bs, _ := json.Marshal(body.Images)
		imagesJSON = datatypes.JSON(bs)
	}
	a, err := h.Svc.CreateAnnouncement(gid, u.ID, body.Title, body.Content, imagesJSON)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[Announcements] Failed to create announcement for guild %d by user %d: %v", gid, u.ID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "创建公告失败"})
		}
		return
	}
	c.JSON(http.StatusOK, a)
}

// @Summary      Get announcement
// @Tags         announcements
// @Security     BearerAuth
// @Produce      json
// @Param        id   path      int  true  "Announcement ID"
// @Success      200   {object}  models.Announcement
// @Failure      401   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Router       /api/announcements/{id} [get]
// getAnnouncement 获取公告
// @Summary      Get announcement
// @Description  获取公告详情
// @Tags         announcements
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id              path  int  true  "Guild ID"
// @Param        announcementId  path  int  true  "Announcement ID"
// @Success      200             {object}  models.Announcement
// @Failure      400             {object}  map[string]string  "参数错误"
// @Failure      401             {object}  map[string]string  "未认证"
// @Failure      403             {object}  map[string]string  "权限不足"
// @Failure      404             {object}  map[string]string  "公告不存在"
// @Failure      500             {object}  map[string]string  "服务器错误"
// @Router       /api/guilds/{id}/announcements/{announcementId} [get]
func (h *HTTP) getAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		logger.Errorf("[Announcements] Invalid announcement ID: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的公告ID"})
		return
	}
	a, err := h.Svc.GetAnnouncement(id)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "公告不存在"})
		} else {
			logger.Errorf("[Announcements] Failed to get announcement %d: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取公告失败"})
		}
		return
	}
	c.JSON(http.StatusOK, a)
}

// @Summary      Update announcement
// @Description  编辑公告（需要管理服务器权限或公告作者本人）
// @Tags         announcements
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id    path  int              true  "Announcement ID"
// @Param        body  body  map[string]string true  "{title?,content?}"
// @Success      200   {object}  models.Announcement
// @Failure      400   {object}  map[string]string  "参数错误"
// @Failure      401   {object}  map[string]string  "未认证"
// @Failure      403   {object}  map[string]string  "权限不足"
// @Failure      404   {object}  map[string]string  "公告不存在"
// @Failure      500   {object}  map[string]string  "服务器错误"
// @Router       /api/announcements/{id} [put]
func (h *HTTP) updateAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		logger.Errorf("[Announcements] Invalid announcement ID: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的公告ID"})
		return
	}
	var body struct {
		Title   *string           `json:"title"`
		Content *string           `json:"content"`
		Images  *[]map[string]any `json:"images"` // [{path, url}], 最多9张; 传空数组可清除图片
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if body.Title == nil && body.Content == nil && body.Images == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "至少需要提供标题、内容或图片"})
		return
	}
	// 校验内容长度
	if body.Content != nil && len([]rune(*body.Content)) > int(config.MaxGuildAnnouncementLength) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "公告内容过长"})
		return
	}
	// 校验图片数量
	var imagesPtr *datatypes.JSON
	if body.Images != nil {
		if int64(len(*body.Images)) > config.MaxAnnouncementImages {
			c.JSON(http.StatusBadRequest, gin.H{"error": "图片数量超过限制(最多9张)"})
			return
		}
		bs, _ := json.Marshal(*body.Images)
		imgJSON := datatypes.JSON(bs)
		imagesPtr = &imgJSON
	}
	a, err := h.Svc.UpdateAnnouncement(id, u.ID, body.Title, body.Content, imagesPtr)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "公告不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[Announcements] Failed to update announcement %d by user %d: %v", id, u.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "编辑公告失败"})
		}
		return
	}
	c.JSON(http.StatusOK, a)
}

// @Summary      Delete announcement
// @Tags         announcements
// @Security     BearerAuth
// @Produce      json
// @Param        id   path      int  true  "Announcement ID"
// @Success      204   {string}  string  ""
// @Failure      401   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Router       /api/announcements/{id} [delete]
// deleteAnnouncement 删除公告
// @Summary      Delete announcement
// @Description  删除公告（需要管理员权限）
// @Tags         announcements
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id              path  int  true  "Guild ID"
// @Param        announcementId  path  int  true  "Announcement ID"
// @Success      200             {object}  map[string]string
// @Failure      400             {object}  map[string]string  "参数错误"
// @Failure      401             {object}  map[string]string  "未认证"
// @Failure      403             {object}  map[string]string  "权限不足"
// @Failure      404             {object}  map[string]string  "公告不存在"
// @Failure      500             {object}  map[string]string  "服务器错误"
// @Router       /api/guilds/{id}/announcements/{announcementId} [delete]
func (h *HTTP) deleteAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		logger.Errorf("[Announcements] Invalid announcement ID: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的公告ID"})
		return
	}
	if err := h.Svc.DeleteAnnouncement(id, u.ID); err != nil {
		if err == service.ErrNotFound {
			// 公告不存在也视为删除成功
			c.Status(http.StatusNoContent)
			return
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		} else {
			logger.Errorf("[Announcements] Failed to delete announcement %d by user %d: %v", id, u.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "删除公告失败"})
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// pinAnnouncement 置顶公告
// @Summary      Pin announcement
// @Description  置顶指定公告（同一服务器只能有一个置顶，会自动取消之前的置顶）
// @Tags         announcements
// @Security     BearerAuth
// @Produce      json
// @Param        id  path  int  true  "Announcement ID"
// @Success      200  {object}  models.Announcement
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/announcements/{id}/pin [put]
func (h *HTTP) pinAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的公告ID"})
		return
	}
	a, err := h.Svc.PinAnnouncement(id, u.ID)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "公告不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[Announcements] Failed to pin announcement %d: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "置顶公告失败"})
		}
		return
	}
	c.JSON(http.StatusOK, a)
}

// unpinAnnouncement 取消置顶公告
// @Summary      Unpin announcement
// @Description  取消置顶公告
// @Tags         announcements
// @Security     BearerAuth
// @Produce      json
// @Param        id  path  int  true  "Announcement ID"
// @Success      204  {string}  string  ""
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/announcements/{id}/pin [delete]
func (h *HTTP) unpinAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的公告ID"})
		return
	}
	if err := h.Svc.UnpinAnnouncement(id, u.ID); err != nil {
		if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "公告不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[Announcements] Failed to unpin announcement %d: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "取消置顶失败"})
		}
		return
	}
	c.Status(http.StatusNoContent)
}

// getFeaturedAnnouncement 获取精选公告（置顶优先，否则最新）
// @Summary      Get featured announcement
// @Description  获取精选公告：如果有置顶公告则返回置顶的，否则返回最新的
// @Tags         announcements
// @Security     BearerAuth
// @Produce      json
// @Param        id  path  int  true  "Guild ID"
// @Success      200  {object}  models.Announcement
// @Failure      401  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/guilds/{id}/announcements/featured [get]
func (h *HTTP) getFeaturedAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	a, err := h.Svc.GetFeaturedAnnouncement(guildID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "暂无公告"})
		return
	}
	c.JSON(http.StatusOK, a)
}
