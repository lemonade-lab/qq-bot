package handlers

import (
	"net/http"
	"strconv"

	"bubble/src/config"
	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// uploadGuildFile 上传文件到服务器文件系统
// @Summary      Upload guild file
// @Description  上传文件到服务器文件系统。默认仅管理员可上传，管理页可设置允许任何人上传。
// @Tags         guild-files
// @Security     BearerAuth
// @Accept       multipart/form-data
// @Produce      json
// @Param        id   path      int   true  "Guild ID"
// @Param        file formData  file  true  "File to upload"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Router       /api/guilds/{id}/files [post]
func (h *HTTP) uploadGuildFile(c *gin.Context) {
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

	// 检查文件大小
	if file.Size > config.MaxGuildFileSize {
		c.JSON(400, gin.H{"error": "文件过大，最大100MB"})
		return
	}

	// 打开文件
	src, err := file.Open()
	if err != nil {
		c.JSON(500, gin.H{"error": "打开文件失败"})
		return
	}
	defer src.Close()

	contentType := file.Header.Get("Content-Type")

	// 上传到MinIO（使用 guild-files 分类）
	ctx := c.Request.Context()
	objectName, errUpload := h.Svc.MinIO.UploadGuildFile(ctx, "guild-files", uint(guildID), src, file.Size, contentType)
	if errUpload != nil {
		logger.Errorf("[GuildFiles] Failed to upload file to MinIO for guild %d: %v", guildID, errUpload)
		c.JSON(500, gin.H{"error": "上传文件失败"})
		return
	}

	// 创建数据库记录
	gf, err := h.Svc.UploadGuildFile(uint(guildID), u.ID, file.Filename, objectName, contentType, file.Size)
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "服务器不存在"})
		} else {
			logger.Errorf("[GuildFiles] Failed to create guild file record for guild %d: %v", guildID, err)
			c.JSON(500, gin.H{"error": "保存文件记录失败"})
		}
		return
	}

	// 填充URL
	gf.FileURL = h.Svc.MinIO.GetFileURL(objectName)

	c.JSON(200, gin.H{"data": gf})
}

// listGuildFiles 获取服务器文件列表（任何成员可查看）
// @Summary      List guild files
// @Description  获取服务器文件列表，任何成员可查看
// @Tags         guild-files
// @Security     BearerAuth
// @Produce      json
// @Param        id     path   int  true   "Guild ID"
// @Param        limit  query  int  false  "每页数量(默认50，最大100)"
// @Param        before query  int  false  "游标：返回ID小于此值的文件"
// @Success      200    {object}  map[string]any
// @Failure      401    {object}  map[string]string
// @Failure      403    {object}  map[string]string
// @Router       /api/guilds/{id}/files [get]
func (h *HTTP) listGuildFiles(c *gin.Context) {
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

	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	before, _ := strconv.ParseUint(c.Query("before"), 10, 64)

	files, err := h.Svc.ListGuildFiles(uint(guildID), u.ID, limit, uint(before))
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "仅服务器成员可查看"})
		} else {
			logger.Errorf("[GuildFiles] Failed to list files for guild %d: %v", guildID, err)
			c.JSON(500, gin.H{"error": "获取文件列表失败"})
		}
		return
	}

	response := gin.H{
		"data": files,
		"pagination": gin.H{
			"limit": limit,
		},
	}
	if len(files) > 0 {
		response["pagination"].(gin.H)["nextCursor"] = files[len(files)-1].ID
	}

	c.JSON(200, response)
}

// deleteGuildFile 删除服务器文件
// @Summary      Delete guild file
// @Description  删除服务器文件（上传者、文件管理权限、管理员或群主）
// @Tags         guild-files
// @Security     BearerAuth
// @Produce      json
// @Param        id      path  int  true  "Guild ID"
// @Param        fileId  path  int  true  "File ID"
// @Success      204     {string}  string  ""
// @Failure      401     {object}  map[string]string
// @Failure      403     {object}  map[string]string
// @Failure      404     {object}  map[string]string
// @Router       /api/guilds/{id}/files/{fileId} [delete]
func (h *HTTP) deleteGuildFile(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	fileID, _ := strconv.ParseUint(c.Param("fileId"), 10, 64)
	if fileID == 0 {
		c.JSON(400, gin.H{"error": "无效的文件ID"})
		return
	}

	if err := h.Svc.DeleteGuildFile(uint(fileID), u.ID); err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "文件不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[GuildFiles] Failed to delete file %d: %v", fileID, err)
			c.JSON(500, gin.H{"error": "删除文件失败"})
		}
		return
	}
	c.Status(http.StatusNoContent)
}

// renameGuildFile 重命名服务器文件
// @Summary      Rename guild file
// @Description  重命名服务器文件（上传者、文件管理权限、管理员或群主）
// @Tags         guild-files
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id      path  int     true  "Guild ID"
// @Param        fileId  path  int     true  "File ID"
// @Param        body    body  object  true  "{fileName: string}"
// @Success      200     {object}  map[string]any
// @Failure      400     {object}  map[string]string
// @Failure      401     {object}  map[string]string
// @Failure      403     {object}  map[string]string
// @Failure      404     {object}  map[string]string
// @Router       /api/guilds/{id}/files/{fileId}/rename [patch]
func (h *HTTP) renameGuildFile(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	fileID, _ := strconv.ParseUint(c.Param("fileId"), 10, 64)
	if fileID == 0 {
		c.JSON(400, gin.H{"error": "无效的文件ID"})
		return
	}

	var body struct {
		FileName string `json:"fileName" binding:"required"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "参数错误：需要 fileName"})
		return
	}
	if len(body.FileName) == 0 || len(body.FileName) > 256 {
		c.JSON(400, gin.H{"error": "文件名长度需在1-256字符之间"})
		return
	}

	f, err := h.Svc.RenameGuildFile(uint(fileID), u.ID, body.FileName)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "文件不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[GuildFiles] Failed to rename file %d: %v", fileID, err)
			c.JSON(500, gin.H{"error": "重命名失败"})
		}
		return
	}

	c.JSON(200, gin.H{"data": f})
}

// batchDeleteGuildFiles 批量删除服务器文件
// @Summary      Batch delete guild files
// @Description  批量删除服务器文件（上传者、文件管理权限、管理员或群主）
// @Tags         guild-files
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id    path  int     true  "Guild ID"
// @Param        body  body  object  true  "{fileIds: [uint]}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Router       /api/guilds/{id}/files/batch-delete [post]
func (h *HTTP) batchDeleteGuildFiles(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	var body struct {
		FileIDs []uint `json:"fileIds" binding:"required"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if len(body.FileIDs) == 0 {
		c.JSON(400, gin.H{"error": "fileIds 不能为空"})
		return
	}
	if len(body.FileIDs) > 100 {
		c.JSON(400, gin.H{"error": "单次最多删除100个文件"})
		return
	}

	succeeded := make([]uint, 0, len(body.FileIDs))
	failed := make([]gin.H, 0)

	for _, fid := range body.FileIDs {
		if err := h.Svc.DeleteGuildFile(fid, u.ID); err != nil {
			reason := "删除失败"
			if err == service.ErrNotFound {
				reason = "文件不存在"
			} else if err == service.ErrUnauthorized {
				reason = "权限不足"
			}
			failed = append(failed, gin.H{"fileId": fid, "error": reason})
		} else {
			succeeded = append(succeeded, fid)
		}
	}

	c.JSON(200, gin.H{
		"succeeded": succeeded,
		"failed":    failed,
	})
}
