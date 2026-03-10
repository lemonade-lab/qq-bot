package handlers

import (
	"bubble/src/db/models"
	"bubble/src/middleware"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

// @Summary      Create moment
// @Description  发布朋友圈
// @Tags         moments
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body  map[string]any  true  "{content,media,location,visibility}"
// @Success      200   {object}  map[string]any  "{\"data\": moment}"
// @Failure      400   {object}  map[string]string  "参数错误"
// @Failure      401   {object}  map[string]string  "未认证"
// @Failure      500   {object}  map[string]string  "服务器错误"
// @Router       /api/moments [post]
func (h *HTTP) createMoment(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	var req struct {
		Content    string          `json:"content"`
		Media      json.RawMessage `json:"media"`
		Location   string          `json:"location"`
		Visibility string          `json:"visibility"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}

	// 验证可见性
	if req.Visibility == "" {
		req.Visibility = "all"
	}
	if req.Visibility != "all" && req.Visibility != "friends" && req.Visibility != "private" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "可见性参数不合法"})
		return
	}

	moment := models.Moment{
		UserID:     u.ID,
		Content:    req.Content,
		Location:   req.Location,
		Visibility: req.Visibility,
	}

	if len(req.Media) > 0 {
		moment.Media = datatypes.JSON(req.Media)
	}

	if err := h.Svc.Repo.DB.Create(&moment).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建朋友圈失败"})
		return
	}

	// 填充用户信息（直接使用已认证的用户，无需重复查询）
	moment.User = u
	// 填充前端兼容字段
	moment.AuthorId = u.ID
	moment.CreatedTs = moment.CreatedAt.UnixMilli()

	c.JSON(http.StatusOK, gin.H{"data": moment})
}

// @Summary      List moments
// @Description  获取朋友圈列表（时间线）
// @Tags         moments
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        limit     query  int  false  "Limit (default 20, max 100)"
// @Param        beforeId  query  int  false  "Return moments with id < beforeId"
// @Param        userId    query  int  false  "Filter by user ID"
// @Success      200       {object}  map[string]any  "{\"data\": []}"
// @Failure      401       {object}  map[string]string  "未认证"
// @Failure      500       {object}  map[string]string  "服务器错误"
// @Router       /api/moments [get]
func (h *HTTP) listMoments(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 获取查询参数
	limitStr := c.DefaultQuery("limit", "20")
	beforeIdStr := c.Query("beforeId")
	userIdStr := c.Query("userId") // 查看指定用户的朋友圈

	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	query := h.Svc.Repo.DB.Model(&models.Moment{}).Where("deleted_at IS NULL")

	// 如果指定了用户ID，只查看该用户的朋友圈
	if userIdStr != "" {
		targetUserId, _ := strconv.ParseUint(userIdStr, 10, 64)
		if targetUserId > 0 {
			// 检查是否是好友或者是自己
			if uint(targetUserId) != u.ID {
				// 检查是否是好友
				var friendship models.Friendship
				err := h.Svc.Repo.DB.Where(
					"(from_user_id = ? AND to_user_id = ? AND status = 'accepted') OR (from_user_id = ? AND to_user_id = ? AND status = 'accepted')",
					u.ID, targetUserId, targetUserId, u.ID,
				).First(&friendship).Error

				if err != nil {
					// 不是好友，只能看到公开的
					query = query.Where("user_id = ? AND visibility = 'all'", targetUserId)
				} else {
					// 是好友，检查隐私模式
					isChatOnly := false
					if friendship.FromUserID == uint(targetUserId) {
						// targetUser是FromUser，检查PrivacyModeFrom
						isChatOnly = friendship.PrivacyModeFrom == "chat_only"
					} else {
						// targetUser是ToUser，检查PrivacyModeTo
						isChatOnly = friendship.PrivacyModeTo == "chat_only"
					}

					if isChatOnly {
						// 设置了仅聊天模式，返回空（不告知对方）
						query = query.Where("1 = 0")
					} else {
						// 正常模式，可以看到公开的和好友可见的
						query = query.Where("user_id = ? AND visibility IN ('all', 'friends')", targetUserId)
					}
				}
			} else {
				// 查看自己的，全部可见
				query = query.Where("user_id = ?", targetUserId)
			}
		}
	} else {
		// 查看朋友圈时间线：自己的 + 好友的
		// 获取所有好友ID
		var friendships []models.Friendship
		h.Svc.Repo.DB.Where(
			"(from_user_id = ? OR to_user_id = ?) AND status = 'accepted'",
			u.ID, u.ID,
		).Find(&friendships)

		friendIds := []uint{u.ID} // 包含自己
		for _, f := range friendships {
			if f.FromUserID == u.ID {
				friendIds = append(friendIds, f.ToUserID)
			} else {
				friendIds = append(friendIds, f.FromUserID)
			}
		}

		// 查询：自己的全部 + 好友的（all 或 friends）
		query = query.Where(
			"(user_id = ?) OR (user_id IN ? AND visibility IN ('all', 'friends'))",
			u.ID, friendIds,
		)
	}

	// beforeId 分页
	if beforeIdStr != "" {
		beforeId, _ := strconv.ParseUint(beforeIdStr, 10, 64)
		if beforeId > 0 {
			query = query.Where("id < ?", beforeId)
		}
	}

	var moments []models.Moment
	if err := query.Order("id DESC").Limit(limit).Find(&moments).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取朋友圈列表失败"})
		return
	}

	if len(moments) == 0 {
		c.JSON(http.StatusOK, gin.H{"data": []models.Moment{}})
		return
	}

	// 批量加载关联数据，避免 N+1 查询
	momentIds := make([]uint, len(moments))
	userIds := make(map[uint]bool)
	for i, m := range moments {
		momentIds[i] = m.ID
		userIds[m.UserID] = true
	}

	// 批量加载用户信息
	var users []models.User
	h.Svc.Repo.DB.Where("id IN ?", keys(userIds)).Find(&users)
	userMap := make(map[uint]*models.User)
	for i := range users {
		userMap[users[i].ID] = &users[i]
	}

	// 批量统计点赞数
	type LikeCount struct {
		MomentID uint
		Count    int64
	}
	var likeCounts []LikeCount
	h.Svc.Repo.DB.Model(&models.MomentLike{}).
		Select("moment_id, COUNT(*) as count").
		Where("moment_id IN ?", momentIds).
		Group("moment_id").
		Scan(&likeCounts)
	likeCountMap := make(map[uint]int)
	for _, lc := range likeCounts {
		likeCountMap[lc.MomentID] = int(lc.Count)
	}

	// 批量统计评论数
	type CommentCount struct {
		MomentID uint
		Count    int64
	}
	var commentCounts []CommentCount
	h.Svc.Repo.DB.Model(&models.MomentComment{}).
		Select("moment_id, COUNT(*) as count").
		Where("moment_id IN ? AND deleted_at IS NULL", momentIds).
		Group("moment_id").
		Scan(&commentCounts)
	commentCountMap := make(map[uint]int)
	for _, cc := range commentCounts {
		commentCountMap[cc.MomentID] = int(cc.Count)
	}

	// 批量检查当前用户点赞状态
	var userLikes []models.MomentLike
	h.Svc.Repo.DB.Where("moment_id IN ? AND user_id = ?", momentIds, u.ID).Find(&userLikes)
	likedMap := make(map[uint]bool)
	for _, ul := range userLikes {
		likedMap[ul.MomentID] = true
	}

	// 批量加载点赞列表（每个朋友圈最多5个）
	var allLikes []models.MomentLike
	h.Svc.Repo.DB.Where("moment_id IN ?", momentIds).Order("moment_id DESC, id DESC").Find(&allLikes)

	// 收集点赞用户ID并加载用户信息
	for _, like := range allLikes {
		userIds[like.UserID] = true
	}

	// 加载点赞用户信息（如果有新的用户ID）
	if len(userIds) > len(userMap) {
		var moreUsers []models.User
		h.Svc.Repo.DB.Where("id IN ?", keys(userIds)).Find(&moreUsers)
		for i := range moreUsers {
			if _, exists := userMap[moreUsers[i].ID]; !exists {
				userMap[moreUsers[i].ID] = &moreUsers[i]
			}
		}
	}

	// 组装点赞列表
	likesByMoment := make(map[uint][]models.MomentLike)
	for _, like := range allLikes {
		if len(likesByMoment[like.MomentID]) < 5 {
			if user, ok := userMap[like.UserID]; ok {
				like.User = user
			}
			likesByMoment[like.MomentID] = append(likesByMoment[like.MomentID], like)
		}
	}

	// 批量加载评论列表（每个朋友圈最多10条）
	var allComments []models.MomentComment
	h.Svc.Repo.DB.Where("moment_id IN ? AND deleted_at IS NULL", momentIds).Order("moment_id ASC, id ASC").Find(&allComments)

	// 收集评论相关的用户ID
	for _, comment := range allComments {
		userIds[comment.UserID] = true
		if comment.ReplyToUser != nil {
			userIds[*comment.ReplyToUser] = true
		}
	}

	// 加载评论用户信息（如果有新的用户ID）
	if len(userIds) > len(userMap) {
		var moreUsers []models.User
		h.Svc.Repo.DB.Where("id IN ?", keys(userIds)).Find(&moreUsers)
		for i := range moreUsers {
			if _, exists := userMap[moreUsers[i].ID]; !exists {
				userMap[moreUsers[i].ID] = &moreUsers[i]
			}
		}
	}

	commentsByMoment := make(map[uint][]models.MomentComment)
	for _, comment := range allComments {
		if len(commentsByMoment[comment.MomentID]) < 10 {
			if user, ok := userMap[comment.UserID]; ok {
				comment.User = user
			}
			if comment.ReplyToUser != nil {
				if replyUser, ok := userMap[*comment.ReplyToUser]; ok {
					comment.ReplyToUserInfo = replyUser
				}
			}
			commentsByMoment[comment.MomentID] = append(commentsByMoment[comment.MomentID], comment)
		}
	}

	// 组装数据
	for i := range moments {
		moments[i].User = userMap[moments[i].UserID]
		moments[i].LikeCount = likeCountMap[moments[i].ID]
		moments[i].CommentCount = commentCountMap[moments[i].ID]
		moments[i].IsLiked = likedMap[moments[i].ID]
		moments[i].Likes = likesByMoment[moments[i].ID]
		moments[i].Comments = commentsByMoment[moments[i].ID]
	}

	c.JSON(http.StatusOK, gin.H{"data": moments})
}

// keys 辅助函数：提取 map 的键
func keys(m map[uint]bool) []uint {
	result := make([]uint, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

// @Summary      Delete moment
// @Description  删除朋友圈
// @Tags         moments
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Moment ID"
// @Success      200  {object}  map[string]string  "{\"message\": \"deleted\"}"
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "权限不足"
// @Failure      404  {object}  map[string]string  "朋友圈不存在"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/moments/{id} [delete]
func (h *HTTP) deleteMoment(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 64)
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的ID"})
		return
	}

	var moment models.Moment
	if err := h.Svc.Repo.DB.First(&moment, id).Error; err != nil {
		// 朋友圈不存在也视为删除成功，避免重复点击报错
		c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
		return
	}

	// 只能删除自己的朋友圈
	if moment.UserID != u.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	now := time.Now()
	moment.DeletedAt = &now
	if err := h.Svc.Repo.DB.Save(&moment).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除朋友圈失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// @Summary      Like moment
// @Description  点赞朋友圈
// @Tags         moments
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Moment ID"
// @Success      200  {object}  map[string]string  "{\"message\": \"liked\"}"
// @Failure      400  {object}  map[string]string  "已点赞或参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      404  {object}  map[string]string  "朋友圈不存在"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/moments/{id}/like [post]
func (h *HTTP) likeMoment(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	idStr := c.Param("id")
	momentId, _ := strconv.ParseUint(idStr, 10, 64)
	if momentId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的ID"})
		return
	}

	// 检查朋友圈是否存在
	var moment models.Moment
	if err := h.Svc.Repo.DB.First(&moment, momentId).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "朋友圈不存在"})
		return
	}

	// 检查是否已点赞
	var existingLike models.MomentLike
	err := h.Svc.Repo.DB.Where("moment_id = ? AND user_id = ?", momentId, u.ID).First(&existingLike).Error
	if err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "已点赞"})
		return
	}

	like := models.MomentLike{
		MomentID: uint(momentId),
		UserID:   u.ID,
	}

	if err := h.Svc.Repo.DB.Create(&like).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "点赞失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "点赞成功"})
}

// @Summary      Unlike moment
// @Description  取消点赞
// @Tags         moments
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Moment ID"
// @Success      200  {object}  map[string]string  "{\"message\": \"unliked\"}"
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/moments/{id}/like [delete]
func (h *HTTP) unlikeMoment(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	idStr := c.Param("id")
	momentId, _ := strconv.ParseUint(idStr, 10, 64)
	if momentId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的ID"})
		return
	}

	// 删除点赞记录
	if err := h.Svc.Repo.DB.Where("moment_id = ? AND user_id = ?", momentId, u.ID).Delete(&models.MomentLike{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "取消点赞失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "取消点赞成功"})
}

// @Summary      Comment on moment
// @Description  评论朋友圈
// @Tags         moments
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Moment ID"
// @Param        body body  map[string]any true "{content,replyToId,replyToUserId}"
// @Success      200  {object}  map[string]any  "{\"data\": comment}"
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      404  {object}  map[string]string  "朋友圈不存在"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/moments/{id}/comments [post]
func (h *HTTP) commentMoment(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	idStr := c.Param("id")
	momentId, _ := strconv.ParseUint(idStr, 10, 64)
	if momentId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的ID"})
		return
	}

	var req struct {
		Content       string `json:"content"`
		ReplyToId     *uint  `json:"replyToId"`
		ReplyToUserId *uint  `json:"replyToUserId"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}

	if req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "评论内容不能为空"})
		return
	}

	// 检查朋友圈是否存在
	var moment models.Moment
	if err := h.Svc.Repo.DB.First(&moment, momentId).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "朋友圈不存在"})
		return
	}

	comment := models.MomentComment{
		MomentID:    uint(momentId),
		UserID:      u.ID,
		Content:     req.Content,
		ReplyToID:   req.ReplyToId,
		ReplyToUser: req.ReplyToUserId,
	}

	if err := h.Svc.Repo.DB.Create(&comment).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "评论失败"})
		return
	}

	// 加载用户信息
	var user models.User
	h.Svc.Repo.DB.First(&user, u.ID)
	comment.User = &user

	if comment.ReplyToUser != nil {
		var replyToUser models.User
		h.Svc.Repo.DB.First(&replyToUser, *comment.ReplyToUser)
		comment.ReplyToUserInfo = &replyToUser
	}

	c.JSON(http.StatusOK, gin.H{"data": comment})
}

// @Summary      Delete comment
// @Description  删除评论
// @Tags         moments
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Comment ID"
// @Success      200  {object}  map[string]string  "{\"message\": \"deleted\"}"
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "权限不足"
// @Failure      404  {object}  map[string]string  "评论不存在"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/moments/comments/{id} [delete]
func (h *HTTP) deleteComment(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 64)
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的ID"})
		return
	}

	var comment models.MomentComment
	if err := h.Svc.Repo.DB.First(&comment, id).Error; err != nil {
		// 评论不存在也视为删除成功，避免重复点击报错
		c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
		return
	}

	// 只能删除自己的评论，或者删除自己朋友圈下的评论
	var moment models.Moment
	h.Svc.Repo.DB.First(&moment, comment.MomentID)

	if comment.UserID != u.ID && moment.UserID != u.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	now := time.Now()
	comment.DeletedAt = &now
	if err := h.Svc.Repo.DB.Save(&comment).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除评论失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// @Summary      List friend's moments
// @Description  获取指定好友的朋友圈列表（仅限好友关系）
// @Tags         moments
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        userId    path   int  true   "好友的用户ID"
// @Param        limit     query  int  false  "Limit (default 20, max 100)"
// @Param        beforeId  query  int  false  "Return moments with id < beforeId"
// @Success      200       {object}  map[string]any  "{\"data\": []}"
// @Failure      400       {object}  map[string]string  "参数错误"
// @Failure      401       {object}  map[string]string  "未认证"
// @Failure      403       {object}  map[string]string  "非好友关系"
// @Failure      404       {object}  map[string]string  "用户不存在"
// @Failure      500       {object}  map[string]string  "服务器错误"
// @Router       /api/friends/{userId}/moments [get]
func (h *HTTP) listFriendMoments(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 解析好友ID
	friendIdStr := c.Param("userId")
	friendId, err := strconv.ParseUint(friendIdStr, 10, 64)
	if err != nil || friendId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的用户ID"})
		return
	}
	targetUserId := uint(friendId)

	// 检查目标用户是否存在
	var targetUser models.User
	if err := h.Svc.Repo.DB.First(&targetUser, targetUserId).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	// 如果是查看自己的，直接返回（也算"好友"）
	if targetUserId != u.ID {
		// 严格检查好友关系（必须是已接受的好友）
		var friendship models.Friendship
		err = h.Svc.Repo.DB.Where(
			"((from_user_id = ? AND to_user_id = ?) OR (from_user_id = ? AND to_user_id = ?)) AND status = 'accepted'",
			u.ID, targetUserId, targetUserId, u.ID,
		).First(&friendship).Error

		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "仅限好友可见"})
			return
		}
	}

	// 获取查询参数
	limitStr := c.DefaultQuery("limit", "20")
	beforeIdStr := c.Query("beforeId")

	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	// 构建查询：查看该用户的朋友圈
	query := h.Svc.Repo.DB.Model(&models.Moment{}).Where("deleted_at IS NULL")

	if targetUserId == u.ID {
		// 查看自己的，全部可见
		query = query.Where("user_id = ?", targetUserId)
	} else {
		// 查看好友的，可以看到公开的和好友可见的
		query = query.Where("user_id = ? AND visibility IN ('all', 'friends')", targetUserId)
	}

	// beforeId 分页
	if beforeIdStr != "" {
		beforeId, _ := strconv.ParseUint(beforeIdStr, 10, 64)
		if beforeId > 0 {
			query = query.Where("id < ?", beforeId)
		}
	}

	var moments []models.Moment
	if err := query.Order("id DESC").Limit(limit).Find(&moments).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取朋友圈列表失败"})
		return
	}

	if len(moments) == 0 {
		c.JSON(http.StatusOK, gin.H{"data": []models.Moment{}})
		return
	}

	// 批量加载关联数据，避免 N+1 查询
	momentIds := make([]uint, len(moments))
	userIds := make(map[uint]bool)
	for i, m := range moments {
		momentIds[i] = m.ID
		userIds[m.UserID] = true
	}

	// 批量加载用户信息
	var users []models.User
	h.Svc.Repo.DB.Where("id IN ?", keys(userIds)).Find(&users)
	userMap := make(map[uint]*models.User)
	for i := range users {
		userMap[users[i].ID] = &users[i]
	}

	// 批量统计点赞数
	type LikeCount struct {
		MomentID uint
		Count    int64
	}
	var likeCounts []LikeCount
	h.Svc.Repo.DB.Model(&models.MomentLike{}).
		Select("moment_id, COUNT(*) as count").
		Where("moment_id IN ?", momentIds).
		Group("moment_id").
		Scan(&likeCounts)
	likeCountMap := make(map[uint]int)
	for _, lc := range likeCounts {
		likeCountMap[lc.MomentID] = int(lc.Count)
	}

	// 批量统计评论数
	type CommentCount struct {
		MomentID uint
		Count    int64
	}
	var commentCounts []CommentCount
	h.Svc.Repo.DB.Model(&models.MomentComment{}).
		Select("moment_id, COUNT(*) as count").
		Where("moment_id IN ? AND deleted_at IS NULL", momentIds).
		Group("moment_id").
		Scan(&commentCounts)
	commentCountMap := make(map[uint]int)
	for _, cc := range commentCounts {
		commentCountMap[cc.MomentID] = int(cc.Count)
	}

	// 批量检查当前用户点赞状态
	var userLikes []models.MomentLike
	h.Svc.Repo.DB.Where("moment_id IN ? AND user_id = ?", momentIds, u.ID).Find(&userLikes)
	likedMap := make(map[uint]bool)
	for _, ul := range userLikes {
		likedMap[ul.MomentID] = true
	}

	// 批量加载点赞列表
	var allLikes []models.MomentLike
	h.Svc.Repo.DB.Where("moment_id IN ?", momentIds).Order("moment_id DESC, id DESC").Find(&allLikes)

	// 收集点赞用户ID并加载用户信息
	for _, like := range allLikes {
		userIds[like.UserID] = true
	}

	// 加载点赞用户信息（如果有新的用户ID）
	if len(userIds) > len(userMap) {
		var moreUsers []models.User
		h.Svc.Repo.DB.Where("id IN ?", keys(userIds)).Find(&moreUsers)
		for i := range moreUsers {
			if _, exists := userMap[moreUsers[i].ID]; !exists {
				userMap[moreUsers[i].ID] = &moreUsers[i]
			}
		}
	}

	// 按朋友圈ID分组点赞
	likesMap := make(map[uint][]models.MomentLike)
	for _, like := range allLikes {
		likesMap[like.MomentID] = append(likesMap[like.MomentID], like)
	}

	// 批量加载评论（每个朋友圈最多前10条）
	type MomentWithComments struct {
		MomentID uint
		Comments []models.MomentComment
	}

	var allComments []models.MomentComment
	h.Svc.Repo.DB.Where("moment_id IN ? AND deleted_at IS NULL", momentIds).
		Order("moment_id DESC, id ASC").
		Find(&allComments)

	// 收集评论用户ID
	for _, comment := range allComments {
		userIds[comment.UserID] = true
		if comment.ReplyToUser != nil {
			userIds[*comment.ReplyToUser] = true
		}
	}

	// 再次加载可能新增的用户
	if len(userIds) > len(userMap) {
		var moreUsers []models.User
		h.Svc.Repo.DB.Where("id IN ?", keys(userIds)).Find(&moreUsers)
		for i := range moreUsers {
			if _, exists := userMap[moreUsers[i].ID]; !exists {
				userMap[moreUsers[i].ID] = &moreUsers[i]
			}
		}
	}

	// 按朋友圈ID分组评论
	commentsMap := make(map[uint][]models.MomentComment)
	for _, comment := range allComments {
		commentsMap[comment.MomentID] = append(commentsMap[comment.MomentID], comment)
	}

	// 构建响应数据
	type MomentResponse struct {
		models.Moment
		User         *models.User           `json:"user"`
		LikeCount    int                    `json:"likeCount"`
		CommentCount int                    `json:"commentCount"`
		Liked        bool                   `json:"liked"`
		Likes        []models.MomentLike    `json:"likes"`
		Comments     []models.MomentComment `json:"comments"`
	}

	result := make([]MomentResponse, len(moments))
	for i, moment := range moments {
		likes := likesMap[moment.ID]
		// 限制返回最多10个点赞
		if len(likes) > 10 {
			likes = likes[:10]
		}
		// 为每个点赞填充用户信息
		for j := range likes {
			if user, ok := userMap[likes[j].UserID]; ok {
				likes[j].User = user
			}
		}

		comments := commentsMap[moment.ID]
		// 限制返回最多10条评论
		if len(comments) > 10 {
			comments = comments[:10]
		}
		// 为每个评论填充用户信息
		for j := range comments {
			if user, ok := userMap[comments[j].UserID]; ok {
				comments[j].User = user
			}
			if comments[j].ReplyToUser != nil {
				if replyToUser, ok := userMap[*comments[j].ReplyToUser]; ok {
					comments[j].ReplyToUserInfo = replyToUser
				}
			}
		}

		result[i] = MomentResponse{
			Moment:       moment,
			User:         userMap[moment.UserID],
			LikeCount:    likeCountMap[moment.ID],
			CommentCount: commentCountMap[moment.ID],
			Liked:        likedMap[moment.ID],
			Likes:        likes,
			Comments:     comments,
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}
