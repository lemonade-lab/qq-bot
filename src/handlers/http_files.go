package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"bubble/src/config"
	"bubble/src/db/models"
	"bubble/src/middleware"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

var imageSize int64 = config.MaxImageSize

var fileSize int64 = config.MaxFileSize

var videoSize int64 = 50 * 1024 * 1024 // 50MB（如需加入常量可后续移动到 config）

// @Summary      Upload file
// @Tags         files
// @Security     BearerAuth
// @Accept       multipart/form-data
// @Produce      json
// @Param        file formData file true "File to upload"
// @Param        category formData string false "File category (avatars, attachments, icons, default: avatars)"
// @Success      200   {object}  map[string]string
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Router       /api/files/upload [post]
func (h *HTTP) uploadFile(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 检查MinIO服务是否可用
	if h.Svc.MinIO == nil {
		c.JSON(500, gin.H{"error": "文件存储服务不可用"})
		return
	}

	// 获取上传的文件
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "缺少文件"})
		return
	}

	// 获取文件分类，默认为 avatars
	category := c.PostForm("category")
	if category == "" {
		category = "avatars"
	}

	// 支持上传到服务器表情（通过 guildId）: 只有服务器 Owner 可以上传频道表情
	guildIdParam := c.PostForm("guildId")

	// 验证文件分类（支持所有分类）
	validCategories := map[string]bool{
		"avatars":            true,
		"covers":             true,
		"emojis":             true,
		"guild-chat-files":   true,
		"private-chat-files": true,
		"temp":               true,
		"bubble":             true,
		"attachments":        true, // 兼容旧分类，映射到 guild-chat-files
		"icons":              true, // 兼容旧分类，映射到 bubble
	}

	// 验证分类合法性
	if !validCategories[category] {
		c.JSON(400, gin.H{"error": "无效的分类，可选：avatars、covers、emojis、guild-chat-files、private-chat-files、temp、attachments、icons"})
		return
	}

	// 验证文件类型（根据分类）
	contentType := file.Header.Get("Content-Type")

	// 以下类型仅限上传图片
	imageCategories := map[string]bool{"avatars": true, "covers": true, "emojis": true, "icons": true}
	// 属于图片
	if imageCategories[category] {
		// 验证图片格式
		if !isImageContentType(contentType) {
			c.JSON(400, gin.H{"error": "文件类型不合法，该分类仅允许图片：" + category})
			return
		}
	}

	// 属于图片
	if imageCategories[category] {
		// 验证图片大小
		if file.Size > imageSize {
			c.JSON(400, gin.H{"error": "图片过大"})
			return
		}
	} else {
		if isVideoContentType(contentType) {
			// 验证视频大小(最大50MB)
			if file.Size > videoSize {
				c.JSON(400, gin.H{"error": "视频过大，最大50MB"})
				return
			}
		} else {
			// 验证其他文件大小
			if file.Size > fileSize {
				c.JSON(400, gin.H{"error": "文件过大"})
				return
			}
		}
	}

	// 打开文件
	src, err := file.Open()
	if err != nil {
		c.JSON(500, gin.H{"error": "打开文件失败"})
		return
	}
	defer src.Close()

	// 上传到MinIO
	ctx := c.Request.Context()
	var objectName string
	var errUpload error

	if category == "emojis" && guildIdParam != "" {
		// 上传到服务器表情：只有服务器 owner 允许
		gid, _ := strconv.ParseUint(guildIdParam, 10, 64)
		var guild models.Guild
		if err := h.Svc.Repo.DB.First(&guild, uint(gid)).Error; err != nil {
			c.JSON(400, gin.H{"error": "服务器不存在"})
			return
		}
		if guild.OwnerID != u.ID {
			c.JSON(403, gin.H{"error": "仅服务器群主可上传服务器表情"})
			return
		}
		objectName, errUpload = h.Svc.MinIO.UploadGuildFile(ctx, category, uint(gid), src, file.Size, contentType)
	} else {
		objectName, errUpload = h.Svc.MinIO.UploadFile(ctx, category, u.ID, src, file.Size, contentType)
	}
	if errUpload != nil {
		c.JSON(500, gin.H{"error": "上传文件失败: " + errUpload.Error()})
		return
	}

	// 返回结构化的文件信息
	fileURL := h.Svc.MinIO.GetFileURL(objectName)
	fileInfo := gin.H{
		"path":        objectName,
		"url":         fileURL,
		"category":    category,
		"size":        file.Size,
		"contentType": contentType,
		"filename":    file.Filename,
	}

	// 检测图片宽高并返回
	if isImageContentType(contentType) {
		if dims, err := h.Svc.MinIO.DetectMediaDimensions(ctx, objectName, contentType); err == nil && dims != nil {
			fileInfo["width"] = dims.Width
			fileInfo["height"] = dims.Height
		}
	}

	c.JSON(200, gin.H{
		"data": gin.H{
			"file": fileInfo,
		},
	})
}

// listFiles 列出当前用户在指定分类下的文件
// GET /api/files/list?category=emojis&userId=123 (userId 可选，默认当前用户)
// @Summary      List files
// @Description  列出用户上传的文件列表
// @Tags         files
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        category  query  string  true   "File category"
// @Param        userId    query  int     false  "User ID (optional, defaults to current user)"
// @Param        guildId   query  int     false  "Guild ID (for guild emojis)"
// @Success      200       {object}  map[string]any
// @Failure      400       {object}  map[string]string  "参数错误"
// @Failure      401       {object}  map[string]string  "未认证"
// @Failure      403       {object}  map[string]string  "权限不足"
// @Failure      500       {object}  map[string]string  "服务器错误"
// @Router       /api/files/list [get]
func (h *HTTP) listFiles(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	category := c.Query("category")
	if category == "" {
		c.JSON(400, gin.H{"error": "缺少分类参数"})
		return
	}

	// 可选指定 userId（仅管理员或自身可用），否则使用当前用户
	userIdParam := c.Query("userId")
	var userID uint = u.ID
	if userIdParam != "" {
		if parsed, err := strconv.ParseUint(userIdParam, 10, 32); err == nil {
			userID = uint(parsed)
		}
	}

	// 支持按服务器列出表情: ?category=emojis&guildId=123
	guildIdParam := c.Query("guildId")

	if h.Svc.MinIO == nil {
		c.JSON(500, gin.H{"error": "文件存储服务不可用"})
		return
	}

	// 如果请求带有 guildId 且 category 是 emojis，则按服务器列出
	if category == "emojis" && guildIdParam != "" {
		gid, err := strconv.ParseUint(guildIdParam, 10, 64)
		if err != nil {
			c.JSON(400, gin.H{"error": "无效的服务器ID"})
			return
		}
		// 只有服务器成员可以查看频道表情
		isMember, err := h.Svc.Repo.IsMember(uint(gid), u.ID)
		if err != nil {
			logger.Errorf("[Files] Failed to check membership for guild %d user %d: %v", gid, u.ID, err)
			c.JSON(500, gin.H{"error": "查询成员状态失败"})
			return
		}
		if !isMember {
			c.JSON(403, gin.H{"error": "非服务器成员"})
			return
		}

		list, err := h.Svc.MinIO.ListGuildFiles(c.Request.Context(), category, uint(gid))
		if err != nil {
			logger.Errorf("[Files] Failed to list guild files for guild %d: %v", gid, err)
			c.JSON(500, gin.H{"error": "获取文件列表失败"})
			return
		}
		c.JSON(200, gin.H{"data": list})
		return
	}

	list, err := h.Svc.MinIO.ListUserFiles(c.Request.Context(), category, userID)
	if err != nil {
		logger.Errorf("[Files] Failed to list user files for user %d: %v", userID, err)
		c.JSON(500, gin.H{"error": "获取文件列表失败"})
		return
	}

	c.JSON(200, gin.H{"data": list})
}

// deleteEmoji 删除当前用户的一项个人表情（安全删除）
// 请求体: { "path": "emojis/123/1704067200.png" } 或 { "path": "emojis/123/1704067200.png" }
// @Summary      Delete emoji
// @Description  删除自定义表情
// @Tags         files
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "{path, guildId}"
// @Success      200   {string}  string
// @Failure      400   {object}  map[string]string  "参数错误"
// @Failure      401   {object}  map[string]string  "未认证"
// @Failure      403   {object}  map[string]string  "权限不足"
// @Failure      404   {object}  map[string]string  "表情不存在"
// @Failure      500   {object}  map[string]string  "服务器错误"
// @Router       /api/emojis/delete [post]
func (h *HTTP) deleteEmoji(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	var body struct {
		Path    string `json:"path"`
		GuildId uint64 `json:"guildId"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误：缺少path"})
		return
	}
	if body.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少path"})
		return
	}

	// 校验路径确保属于当前用户: 支持两种格式 - "emojis/{userID}/..." 或 "{bucket}/emojis/{userID}/..."
	parts := strings.Split(body.Path, "/")
	if len(parts) < 3 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "路径格式错误"})
		return
	}

	// If client explicitly provided a guildId, use it for permission check (guild emoji deletion).
	if body.GuildId != 0 {
		var guild models.Guild
		if err := h.Svc.Repo.DB.First(&guild, uint(body.GuildId)).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "服务器不存在"})
			return
		}
		if guild.OwnerID != u.ID {
			c.JSON(http.StatusForbidden, gin.H{"error": "仅服务器群主可删除服务器表情"})
			return
		}
	} else {
		// Fallback: try to infer owner/user id from path (legacy support)
		var ownerID uint64
		for i, p := range parts {
			if p == "emojis" && i+1 < len(parts) {
				if id, err := strconv.ParseUint(parts[i+1], 10, 64); err == nil {
					ownerID = id
					break
				}
			}
		}
		if ownerID == 0 || uint(ownerID) != u.ID {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权删除该表情"})
			return
		}
	}

	if h.Svc.MinIO == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "文件存储服务不可用"})
		return
	}

	if err := h.Svc.MinIO.DeleteFile(c.Request.Context(), body.Path); err != nil {
		logger.Errorf("[Files] Failed to delete file %s: %v", body.Path, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除文件失败"})
		return
	}

	c.Status(http.StatusOK)
}

// isImageContentType 检查Content-Type是否为图片类型
func isImageContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "image/") &&
		(strings.Contains(contentType, "jpeg") ||
			strings.Contains(contentType, "jpg") ||
			strings.Contains(contentType, "png") ||
			strings.Contains(contentType, "gif") ||
			strings.Contains(contentType, "webp"))
}

// 检查是否是视频类型
func isVideoContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "video/") &&
		(strings.Contains(contentType, "mp4") ||
			strings.Contains(contentType, "webm") ||
			strings.Contains(contentType, "ogg") ||
			strings.Contains(contentType, "quicktime") ||
			strings.Contains(contentType, "mov"))
}
