package handlers

import (
	"bubble/src/db/models"
	"bubble/src/middleware"
	"bubble/src/service"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"bubble/src/config"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ==================== Forum Posts ====================

// @Summary      List forum posts
// @Tags         forum
// @Security     BearerAuth
// @Produce      json
// @Param        id       path   int     true   "Channel ID"
// @Param        limit    query  int     false  "Limit (default 20)"
// @Param        offset   query  int     false  "Offset (default 0)"
// @Param        sortBy   query  string  false  "Sort by: latest, oldest, popular (default latest)"
// @Success      200      {array}   models.ForumPost
// @Failure      400      {object}  map[string]string
// @Failure      401      {object}  map[string]string
// @Failure      403      {object}  map[string]string
// @Failure      404      {object}  map[string]string
// @Router       /api/channels/{id}/posts [get]
func (h *HTTP) listForumPosts(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 解析频道 ID
	channelIDStr := c.Param("id")
	channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	// 获取频道信息并验证类型
	var channel models.Channel
	if err := h.Svc.Repo.DB.First(&channel, channelID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "频道不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库错误"})
		return
	}

	// 检查频道类型
	if channel.Type != "forum" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该频道不是论坛频道"})
		return
	}

	// 检查用户是否是服务器成员
	isMember, _ := h.Svc.Repo.IsMember(channel.GuildID, u.ID)
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 解析查询参数
	limit := 20
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	offset := 0
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	sortBy := c.DefaultQuery("sortBy", "latest")

	// 构建查询
	query := h.Svc.Repo.DB.Model(&models.ForumPost{}).
		Where("channel_id = ?", channelID).
		Where("deleted_at IS NULL").
		Preload("Author")

	// 排序
	switch sortBy {
	case "oldest":
		query = query.Order("is_pinned DESC, created_at ASC")
	case "popular":
		query = query.Order("is_pinned DESC, view_count DESC, reply_count DESC")
	default: // latest
		query = query.Order("is_pinned DESC, created_at DESC")
	}

	// 查询
	var posts []models.ForumPost
	if err := query.Limit(limit).Offset(offset).Find(&posts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取帖子列表失败"})
		return
	}

	// 批量加载关联数据，避免 N+1 查询
	postIDs := make([]uint, len(posts))
	for i, p := range posts {
		postIDs[i] = p.ID
	}

	// 批量统计点赞数
	type LikeCount struct {
		PostID uint
		Count  int
	}
	var LikeCounts []LikeCount
	h.Svc.Repo.DB.Model(&models.ForumPostLike{}).Select("post_id, COUNT(*) as Count").
		Where("post_id IN ? AND is_liked = 1", postIDs).
		Group("post_id").
		Scan(&LikeCounts)
	LikeCountMap := make(map[uint]int)
	for _, lc := range LikeCounts {
		LikeCountMap[lc.PostID] = lc.Count
	}

	// 拉取自己点赞过的帖子列表
	type Liked struct {
		PostID uint
		Liked  bool
	}
	var LikedList []Liked
	h.Svc.Repo.DB.Model(&models.ForumPostLike{}).Select("post_id, is_liked as Liked").
		Where("post_id IN ? AND operator_user_id = ?", postIDs, u.ID).
		Scan(&LikedList)
	LikedMap := make(map[uint]bool)
	for _, lc := range LikedList {
		if lc.Liked {
			LikedMap[lc.PostID] = true
		}
	}

	// 额外数据组装
	for i, lc := range posts {
		posts[i].LikeCount = LikeCountMap[lc.ID]
		posts[i].IsLiked = LikedMap[lc.ID]
	}

	c.JSON(http.StatusOK, posts)
}

// @Summary      Get forum post
// @Tags         forum
// @Security     BearerAuth
// @Produce      json
// @Param        id       path  int  true  "Channel ID"
// @Param        postId   path  int  true  "Post ID"
// @Success      200      {object}  models.ForumPost
// @Failure      400      {object}  map[string]string
// @Failure      401      {object}  map[string]string
// @Failure      403      {object}  map[string]string
// @Failure      404      {object}  map[string]string
// @Router       /api/channels/{id}/posts/{postId} [get]
func (h *HTTP) getForumPost(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 解析频道 ID 和帖子 ID
	channelIDStr := c.Param("id")
	channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	postIDStr := c.Param("postId")
	postID, err := strconv.ParseUint(postIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的帖子ID"})
		return
	}

	// 获取频道信息
	var channel models.Channel
	if err := h.Svc.Repo.DB.First(&channel, channelID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "频道不存在"})
		return
	}

	// 检查用户权限
	isMember, _ := h.Svc.Repo.IsMember(channel.GuildID, u.ID)
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 获取帖子
	var post models.ForumPost
	if err := h.Svc.Repo.DB.Where("id = ? AND channel_id = ? AND deleted_at IS NULL", postID, channelID).
		Preload("Author").
		First(&post).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "帖子不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库错误"})
		return
	}

	// 获取点赞次数
	h.Svc.Repo.DB.Model(&models.ForumPostLike{}).Select("COUNT(*) as Count").
		Where("post_id = ? AND is_liked = 1", postID).
		Scan(&post.LikeCount)

	// 获取自己是否已经点赞
	h.Svc.Repo.DB.Model(&models.ForumPostLike{}).Select("is_liked as Liked").
		Where("post_id = ? AND operator_user_id = ?", postID, u.ID).
		Scan(&post.IsLiked)

	// 增加浏览次数
	h.Svc.Repo.DB.Model(&post).UpdateColumn("view_count", gorm.Expr("view_count + 1"))

	c.JSON(http.StatusOK, post)
}

// @Summary      Like forum post
// @Tags         forum
// @Security     BearerAuth
// @Produce      json
// @Param        id       path  int  true  "Channel ID"
// @Param        postId   path  int  true  "Post ID"
// @Param        body     body  object  true  "{isLike}"
// @Success      200      {object}  models.ForumPostLike
// @Failure      400      {object}  map[string]string
// @Failure      401      {object}  map[string]string
// @Failure      403      {object}  map[string]string
// @Failure      404      {object}  map[string]string
// @Router       /api/channels/{id}/posts/{postId}/like [post]
func (h *HTTP) likeForumPost(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 解析频道 ID 和帖子 ID
	channelIDStr := c.Param("id")
	channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	postIDStr := c.Param("postId")
	postID, err := strconv.ParseUint(postIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的帖子ID"})
		return
	}

	// 获取频道信息
	var channel models.Channel
	if err := h.Svc.Repo.DB.First(&channel, channelID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "频道不存在"})
		return
	}

	// 检查用户权限
	isMember, _ := h.Svc.Repo.IsMember(channel.GuildID, u.ID)
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 获取需要更新的帖子点赞状态
	var body struct {
		IsLike bool `json:"is_like"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的帖子点赞状态"})
		return
	}

	like := models.ForumPostLike{
		PostID:         uint(postID),
		OperatorUserID: u.ID,
		IsLiked:        body.IsLike,
	}
	if err := h.Svc.Repo.DB.Save(&like).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "点赞帖子失败"})
		return
	}

	c.JSON(http.StatusOK, like)
}

// @Summary      Create forum post
// @Tags         forum
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id    path  int     true  "Channel ID"
// @Param        body  body  object  true  "{title, content}"
// @Success      201   {object}  models.ForumPost
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Router       /api/channels/{id}/posts [post]
func (h *HTTP) createForumPost(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 解析频道 ID
	channelIDStr := c.Param("id")
	channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	// 获取频道信息
	var channel models.Channel
	if err := h.Svc.Repo.DB.First(&channel, channelID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "频道不存在"})
		return
	}

	// 检查频道类型
	if channel.Type != "forum" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该频道不是论坛频道"})
		return
	}

	// 检查用户权限
	isMember, _ := h.Svc.Repo.IsMember(channel.GuildID, u.ID)
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 解析请求体
	var input struct {
		Title   string      `json:"title" binding:"required"`
		Content string      `json:"content" binding:"required"`
		Media   interface{} `json:"media"` // 媒体附件 [{type,url}]，最多9项
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	// 长度限制
	if l := len([]rune(input.Title)); l == 0 || l > int(config.MaxForumPostTitleLength) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "标题长度不合法"})
		return
	}
	if len([]rune(input.Content)) > int(config.MaxForumPostContentLength) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "内容过长"})
		return
	}

	// 序列化媒体数据
	var mediaJSON datatypes.JSON
	if input.Media != nil {
		bs, _ := json.Marshal(input.Media)
		mediaJSON = datatypes.JSON(bs)
	}

	// 创建帖子
	post := models.ForumPost{
		ChannelID: uint(channelID),
		AuthorID:  u.ID,
		Title:     input.Title,
		Content:   input.Content,
		Media:     mediaJSON,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := h.Svc.Repo.DB.Create(&post).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建帖子失败"})
		return
	}

	// 加载作者信息
	h.Svc.Repo.DB.Preload("Author").First(&post, post.ID)

	c.JSON(http.StatusCreated, post)
}

// @Summary      Update forum post
// @Description  更新论坛帖子（仅作者）
// @Tags         forum
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id      path  int     true  "Channel ID"
// @Param        postId  path  int     true  "Post ID"
// @Param        body    body  object  true  "{title, content}"
// @Success      200     {object}  models.ForumPost
// @Failure      400     {object}  map[string]string  "参数错误"
// @Failure      401     {object}  map[string]string  "未认证"
// @Failure      403     {object}  map[string]string  "权限不足"
// @Failure      404     {object}  map[string]string  "帖子不存在"
// @Failure      500     {object}  map[string]string  "服务器错误"
// @Router       /api/channels/{id}/posts/{postId} [put]
func (h *HTTP) updateForumPost(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 解析 ID
	channelIDStr := c.Param("id")
	channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	postIDStr := c.Param("postId")
	postID, err := strconv.ParseUint(postIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的帖子ID"})
		return
	}

	// 获取帖子
	var post models.ForumPost
	if err := h.Svc.Repo.DB.Where("id = ? AND channel_id = ? AND deleted_at IS NULL", postID, channelID).
		First(&post).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "帖子不存在"})
		return
	}

	// 检查权限（只有作者可以编辑）
	if post.AuthorID != u.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 解析请求体
	var input struct {
		Title   *string `json:"title"`
		Content *string `json:"content"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}

	// 更新
	updates := make(map[string]interface{})
	if input.Title != nil {
		updates["title"] = *input.Title
	}
	if input.Content != nil {
		updates["content"] = *input.Content
	}
	updates["updated_at"] = time.Now()

	if err := h.Svc.Repo.DB.Model(&post).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新帖子失败"})
		return
	}

	// 重新加载
	h.Svc.Repo.DB.Preload("Author").First(&post, post.ID)

	c.JSON(http.StatusOK, post)
}

// @Summary      Delete forum post
// @Description  删除论坛帖子（仅作者）
// @Tags         forum
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id      path  int  true  "Channel ID"
// @Param        postId  path  int  true  "Post ID"
// @Success      200     {object}  map[string]string
// @Failure      400     {object}  map[string]string  "参数错误"
// @Failure      401     {object}  map[string]string  "未认证"
// @Failure      403     {object}  map[string]string  "权限不足"
// @Failure      404     {object}  map[string]string  "帖子不存在"
// @Failure      500     {object}  map[string]string  "服务器错误"
// @Router       /api/channels/{id}/posts/{postId} [delete]
func (h *HTTP) deleteForumPost(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 解析 ID
	channelIDStr := c.Param("id")
	channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	postIDStr := c.Param("postId")
	postID, err := strconv.ParseUint(postIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的帖子ID"})
		return
	}

	// 获取帖子
	var post models.ForumPost
	if err := h.Svc.Repo.DB.Where("id = ? AND channel_id = ? AND deleted_at IS NULL", postID, channelID).
		First(&post).Error; err != nil {
		// 帖子不存在也视为删除成功
		c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
		return
	}

	// 检查权限（作者或有管理频道权限的管理员可以删除）
	if post.AuthorID != u.ID {
		// 获取频道信息以检查公会权限
		var channel models.Channel
		if err := h.Svc.Repo.DB.First(&channel, channelID).Error; err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		}
		hasManageChannels, err := h.Svc.HasGuildPerm(channel.GuildID, u.ID, service.PermManageChannels)
		if err != nil || !hasManageChannels {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		}
	}

	// 软删除
	now := time.Now()
	if err := h.Svc.Repo.DB.Model(&post).Update("deleted_at", now).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除帖子失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// @Summary      Pin forum post
// @Description  置顶论坛帖子（需要管理频道权限）
// @Tags         forum
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id      path  int  true  "Channel ID"
// @Param        postId  path  int  true  "Post ID"
// @Success      200     {object}  map[string]string
// @Failure      400     {object}  map[string]string  "参数错误"
// @Failure      401     {object}  map[string]string  "未认证"
// @Failure      403     {object}  map[string]string  "权限不足"
// @Failure      404     {object}  map[string]string  "帖子不存在"
// @Failure      500     {object}  map[string]string  "服务器错误"
// @Router       /api/channels/{id}/posts/{postId}/pin [post]
func (h *HTTP) pinForumPost(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 解析 ID
	channelIDStr := c.Param("id")
	channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	postIDStr := c.Param("postId")
	postID, err := strconv.ParseUint(postIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的帖子ID"})
		return
	}

	// 获取频道和帖子
	var channel models.Channel
	if err := h.Svc.Repo.DB.First(&channel, channelID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "频道不存在"})
		return
	}

	var post models.ForumPost
	if err := h.Svc.Repo.DB.Where("id = ? AND channel_id = ? AND deleted_at IS NULL", postID, channelID).
		First(&post).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "帖子不存在"})
		return
	}

	// 检查权限（需要管理频道权限）
	hasManageChannels, err := h.Svc.HasGuildPerm(channel.GuildID, u.ID, service.PermManageChannels)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "权限校验失败"})
		return
	}
	if !hasManageChannels {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 切换置顶状态
	newPinned := !post.IsPinned
	if err := h.Svc.Repo.DB.Model(&post).Update("is_pinned", newPinned).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "置顶操作失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "置顶状态已更新", "isPinned": newPinned})
}

// @Summary      Lock forum post
// @Description  锁定论坛帖子（需要管理频道权限）
// @Tags         forum
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id      path  int  true  "Channel ID"
// @Param        postId  path  int  true  "Post ID"
// @Success      200     {object}  map[string]string
// @Failure      400     {object}  map[string]string  "参数错误"
// @Failure      401     {object}  map[string]string  "未认证"
// @Failure      403     {object}  map[string]string  "权限不足"
// @Failure      404     {object}  map[string]string  "帖子不存在"
// @Failure      500     {object}  map[string]string  "服务器错误"
// @Router       /api/channels/{id}/posts/{postId}/lock [post]
func (h *HTTP) lockForumPost(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 解析 ID
	channelIDStr := c.Param("id")
	channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	postIDStr := c.Param("postId")
	postID, err := strconv.ParseUint(postIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的帖子ID"})
		return
	}

	// 获取频道和帖子
	var channel models.Channel
	if err := h.Svc.Repo.DB.First(&channel, channelID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "频道不存在"})
		return
	}

	var post models.ForumPost
	if err := h.Svc.Repo.DB.Where("id = ? AND channel_id = ? AND deleted_at IS NULL", postID, channelID).
		First(&post).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "帖子不存在"})
		return
	}

	// 检查权限（需要管理频道权限）
	hasManageChannels, err := h.Svc.HasGuildPerm(channel.GuildID, u.ID, service.PermManageChannels)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "权限校验失败"})
		return
	}
	if !hasManageChannels {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 切换锁定状态
	newLocked := !post.IsLocked
	if err := h.Svc.Repo.DB.Model(&post).Update("is_locked", newLocked).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "锁定操作失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "锁定状态已更新", "isLocked": newLocked})
}

// ==================== Forum Replies ====================

// @Summary      List forum replies
// @Description  列出论坛帖子的回复
// @Tags         forum
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id       path   int  true   "Channel ID"
// @Param        postId   path   int  true   "Post ID"
// @Param        limit    query  int  false  "Limit (default 50)"
// @Param        offset   query  int  false  "Offset (default 0)"
// @Success      200      {array}   models.ForumReply
// @Failure      400      {object}  map[string]string  "参数错误"
// @Failure      401      {object}  map[string]string  "未认证"
// @Failure      403      {object}  map[string]string  "权限不足"
// @Failure      404      {object}  map[string]string  "帖子不存在"
// @Failure      500      {object}  map[string]string  "服务器错误"
// @Router       /api/channels/{id}/posts/{postId}/replies [get]
func (h *HTTP) listForumReplies(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 解析 ID
	channelIDStr := c.Param("id")
	channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	postIDStr := c.Param("postId")
	postID, err := strconv.ParseUint(postIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的帖子ID"})
		return
	}

	// 获取频道
	var channel models.Channel
	if err := h.Svc.Repo.DB.First(&channel, channelID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "频道不存在"})
		return
	}

	// 检查权限
	isMember, _ := h.Svc.Repo.IsMember(channel.GuildID, u.ID)
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 验证帖子存在
	var post models.ForumPost
	if err := h.Svc.Repo.DB.Where("id = ? AND channel_id = ? AND deleted_at IS NULL", postID, channelID).
		First(&post).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "帖子不存在"})
		return
	}

	// 解析查询参数
	limit := 50
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	offset := 0
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// 查询回复
	var replies []models.ForumReply
	if err := h.Svc.Repo.DB.Where("post_id = ? AND deleted_at IS NULL", postID).
		Preload("Author").
		Order("created_at ASC").
		Limit(limit).
		Offset(offset).
		Find(&replies).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取回复列表失败"})
		return
	}

	c.JSON(http.StatusOK, replies)
}

// @Summary      Create forum reply
// @Description  创建论坛回复
// @Tags         forum
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id      path  int     true  "Channel ID"
// @Param        postId  path  int     true  "Post ID"
// @Param        body    body  object  true  "{content}"
// @Success      201     {object}  models.ForumReply
// @Failure      400     {object}  map[string]string  "参数错误"
// @Failure      401     {object}  map[string]string  "未认证"
// @Failure      403     {object}  map[string]string  "权限不足或帖子已锁定"
// @Failure      404     {object}  map[string]string  "帖子不存在"
// @Failure      500     {object}  map[string]string  "服务器错误"
// @Router       /api/channels/{id}/posts/{postId}/replies [post]
func (h *HTTP) createForumReply(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 解析 ID
	channelIDStr := c.Param("id")
	channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	postIDStr := c.Param("postId")
	postID, err := strconv.ParseUint(postIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的帖子ID"})
		return
	}

	// 获取频道
	var channel models.Channel
	if err := h.Svc.Repo.DB.First(&channel, channelID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "频道不存在"})
		return
	}

	// 检查权限
	isMember, _ := h.Svc.Repo.IsMember(channel.GuildID, u.ID)
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 获取帖子并检查是否锁定
	var post models.ForumPost
	if err := h.Svc.Repo.DB.Where("id = ? AND channel_id = ? AND deleted_at IS NULL", postID, channelID).
		First(&post).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "帖子不存在"})
		return
	}

	if post.IsLocked {
		c.JSON(http.StatusForbidden, gin.H{"error": "帖子已锁定"})
		return
	}

	// 解析请求体
	var input struct {
		Content   string      `json:"content" binding:"required"`
		ReplyToID *uint       `json:"replyToId"`
		Type      string      `json:"type"`     // text / voice / image
		FileMeta  interface{} `json:"fileMeta"` // 语音/图片文件元数据
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if l := len([]rune(input.Content)); l == 0 || l > int(config.MaxForumReplyLength) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "回复长度不合法"})
		return
	}

	// 处理回复类型
	replyType := "text"
	if input.Type == "voice" || input.Type == "image" {
		replyType = input.Type
	}

	// 序列化 fileMeta
	var fileMetaJSON datatypes.JSON
	if input.FileMeta != nil {
		bs, _ := json.Marshal(input.FileMeta)
		fileMetaJSON = datatypes.JSON(bs)
	}

	// 创建回复
	reply := models.ForumReply{
		PostID:    uint(postID),
		AuthorID:  u.ID,
		Content:   input.Content,
		Type:      replyType,
		FileMeta:  fileMetaJSON,
		ReplyToID: input.ReplyToID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := h.Svc.Repo.DB.Create(&reply).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建回复失败"})
		return
	}

	// 更新帖子回复数
	h.Svc.Repo.DB.Model(&post).UpdateColumn("reply_count", gorm.Expr("reply_count + 1"))

	// 加载作者信息
	h.Svc.Repo.DB.Preload("Author").First(&reply, reply.ID)

	c.JSON(http.StatusCreated, reply)
}

// @Summary      Delete forum reply
// @Description  删除论坛回复（仅作者）
// @Tags         forum
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id       path  int  true  "Channel ID"
// @Param        postId   path  int  true  "Post ID"
// @Param        replyId  path  int  true  "Reply ID"
// @Success      200      {object}  map[string]string
// @Failure      400      {object}  map[string]string  "参数错误"
// @Failure      401      {object}  map[string]string  "未认证"
// @Failure      403      {object}  map[string]string  "权限不足"
// @Failure      404      {object}  map[string]string  "回复不存在"
// @Failure      500      {object}  map[string]string  "服务器错误"
// @Router       /api/channels/{id}/posts/{postId}/replies/{replyId} [delete]
func (h *HTTP) deleteForumReply(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 解析 ID
	postIDStr := c.Param("postId")
	postID, err := strconv.ParseUint(postIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的帖子ID"})
		return
	}

	replyIDStr := c.Param("replyId")
	replyID, err := strconv.ParseUint(replyIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的回复ID"})
		return
	}

	// 获取回复
	var reply models.ForumReply
	if err := h.Svc.Repo.DB.Where("id = ? AND post_id = ? AND deleted_at IS NULL", replyID, postID).
		First(&reply).Error; err != nil {
		// 回复不存在也视为删除成功
		c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
		return
	}

	// 检查权限（作者或有管理频道权限的管理员可以删除）
	if reply.AuthorID != u.ID {
		channelIDStr := c.Param("id")
		channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		}
		var channel models.Channel
		if err := h.Svc.Repo.DB.First(&channel, channelID).Error; err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		}
		hasManageChannels, err := h.Svc.HasGuildPerm(channel.GuildID, u.ID, service.PermManageChannels)
		if err != nil || !hasManageChannels {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		}
	}

	// 软删除
	now := time.Now()
	if err := h.Svc.Repo.DB.Model(&reply).Update("deleted_at", now).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除回复失败"})
		return
	}

	// 更新帖子回复数
	var post models.ForumPost
	if err := h.Svc.Repo.DB.First(&post, postID).Error; err == nil {
		h.Svc.Repo.DB.Model(&post).UpdateColumn("reply_count", gorm.Expr("reply_count - 1"))
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}
