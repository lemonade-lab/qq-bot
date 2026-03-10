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

// ==================== 群聊加入申请 ====================

// applyGroupJoin 申请加入群聊
func (h *HTTP) applyGroupJoin(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, err := parseUintParam(c, "id")
	if err != nil || tid == 0 {
		c.JSON(400, gin.H{"error": "无效的群聊ID"})
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if err := c.BindJSON(&body); err != nil {
		body.Note = ""
	}

	req, err := h.Svc.ApplyGroupJoin(tid, u.ID, body.Note)
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[Group] Failed to apply join group %d by user %d: %v", tid, u.ID, err)
			c.JSON(400, gin.H{"error": "申请加入群聊失败"})
		}
		return
	}

	// req == nil 表示自由加入模式，已直接加入
	if req == nil {
		// 广播成员加入事件
		if h.Gw != nil {
			memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(tid)
			h.Gw.BroadcastToUsers(memberIDs, config.EventGroupMemberAdd, gin.H{
				"threadId": tid,
				"userId":   u.ID,
				"user": gin.H{
					"id":     u.ID,
					"name":   u.Name,
					"avatar": u.Avatar,
				},
			})
		}
		c.JSON(200, gin.H{"status": "joined", "message": "已加入"})
		return
	}

	// 推送通知给群主
	if h.Gw != nil {
		thread, _ := h.Svc.Repo.GetGroupThread(tid)
		if thread != nil {
			h.Gw.BroadcastNotice(thread.OwnerID, gin.H{
				"type":       "group_join_request",
				"sourceType": "system",
				"threadId":   tid,
				"status":     "pending",
				"read":       false,
				"thread": gin.H{
					"id":     thread.ID,
					"name":   thread.Name,
					"avatar": thread.Avatar,
					"banner": thread.Banner,
				},
				"author": gin.H{
					"id":     u.ID,
					"name":   u.Name,
					"avatar": u.Avatar,
					"bio":    u.Bio,
					"banner": u.Banner,
				},
			})
		}
	}

	c.JSON(200, req)
}

// listGroupJoinRequests 列出群聊加入申请（群主/管理员）
func (h *HTTP) listGroupJoinRequests(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, err := parseUintParam(c, "id")
	if err != nil || tid == 0 {
		c.JSON(400, gin.H{"error": "无效的群聊ID"})
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > 100 {
		limit = 10
	}

	requests, total, err := h.Svc.ListGroupJoinRequests(tid, u.ID, page, limit)
	if err != nil {
		if err == service.ErrForbidden {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[Group] Failed to list join requests for group %d: %v", tid, err)
			c.JSON(400, gin.H{"error": "获取申请列表失败"})
		}
		return
	}

	c.JSON(200, gin.H{
		"data": requests,
		"pagination": gin.H{
			"page":    page,
			"limit":   limit,
			"total":   total,
			"hasMore": page*limit < total,
		},
	})
}

// getUserGroupJoinRequestStatus 获取当前用户的群聊加入申请状态
func (h *HTTP) getUserGroupJoinRequestStatus(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, err := parseUintParam(c, "id")
	if err != nil || tid == 0 {
		c.JSON(400, gin.H{"error": "无效的群聊ID"})
		return
	}
	req, err := h.Svc.GetUserGroupJoinRequestStatus(tid, u.ID)
	if err != nil {
		c.JSON(200, nil)
		return
	}
	c.JSON(200, req)
}

// approveGroupJoin 批准群聊加入申请
func (h *HTTP) approveGroupJoin(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, err := parseUintParam(c, "id")
	if err != nil || tid == 0 {
		c.JSON(400, gin.H{"error": "无效的群聊ID"})
		return
	}
	requestID, err := strconv.ParseUint(c.Param("requestId"), 10, 64)
	if err != nil || requestID == 0 {
		c.JSON(400, gin.H{"error": "无效的请求ID"})
		return
	}

	req, _ := h.Svc.Repo.GetGroupJoinRequestByID(uint(requestID))

	if err := h.Svc.ApproveGroupJoinRequest(tid, uint(requestID), u.ID); err != nil {
		if err == service.ErrForbidden || err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[Group] Failed to approve join request %d for group %d: %v", requestID, tid, err)
			c.JSON(400, gin.H{"error": "批准申请失败"})
		}
		return
	}

	// 广播新成员加入事件
	if h.Gw != nil && req != nil {
		memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(tid)
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupMemberAdd, gin.H{
			"threadId": tid,
			"userId":   req.UserID,
		})

		// 通知申请者
		thread, _ := h.Svc.Repo.GetGroupThread(tid)
		notifPayload := gin.H{
			"type":       "group_join_approved",
			"sourceType": "system",
			"threadId":   tid,
			"requestId":  requestID,
			"status":     "approved",
			"read":       false,
		}
		if thread != nil {
			notifPayload["thread"] = gin.H{
				"id":     thread.ID,
				"name":   thread.Name,
				"avatar": thread.Avatar,
			}
		}
		h.Gw.BroadcastNotice(req.UserID, notifPayload)
	}

	c.JSON(200, gin.H{"success": true})
}

// rejectGroupJoin 拒绝群聊加入申请
func (h *HTTP) rejectGroupJoin(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, err := parseUintParam(c, "id")
	if err != nil || tid == 0 {
		c.JSON(400, gin.H{"error": "无效的群聊ID"})
		return
	}
	requestID, err := strconv.ParseUint(c.Param("requestId"), 10, 64)
	if err != nil || requestID == 0 {
		c.JSON(400, gin.H{"error": "无效的请求ID"})
		return
	}

	req, _ := h.Svc.Repo.GetGroupJoinRequestByID(uint(requestID))

	if err := h.Svc.RejectGroupJoinRequest(tid, uint(requestID), u.ID); err != nil {
		if err == service.ErrForbidden || err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[Group] Failed to reject join request %d for group %d: %v", requestID, tid, err)
			c.JSON(400, gin.H{"error": "拒绝申请失败"})
		}
		return
	}

	// 通知申请者被拒绝
	if h.Gw != nil && req != nil {
		thread, _ := h.Svc.Repo.GetGroupThread(tid)
		notifPayload := gin.H{
			"type":       "group_join_rejected",
			"sourceType": "system",
			"threadId":   tid,
			"requestId":  requestID,
			"status":     "rejected",
			"read":       false,
		}
		if thread != nil {
			notifPayload["thread"] = gin.H{
				"id":     thread.ID,
				"name":   thread.Name,
				"avatar": thread.Avatar,
			}
		}
		h.Gw.BroadcastNotice(req.UserID, notifPayload)
	}

	c.JSON(200, gin.H{"success": true})
}

// ==================== 群公告 ====================

// listGroupAnnouncements 列出群公告
func (h *HTTP) listGroupAnnouncements(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的群聊ID"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit < 1 || limit > 100 {
		limit = 10
	}

	list, total, err := h.Svc.ListGroupAnnouncements(tid, u.ID, page, limit)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "仅群成员可查看"})
		} else {
			logger.Errorf("[Group] Failed to list announcements for group %d: %v", tid, err)
			c.JSON(500, gin.H{"error": "获取公告列表失败"})
		}
		return
	}

	c.JSON(200, gin.H{
		"data": list,
		"pagination": gin.H{
			"page":    page,
			"limit":   limit,
			"total":   total,
			"hasMore": page*limit < total,
		},
	})
}

// createGroupAnnouncement 创建群公告
func (h *HTTP) createGroupAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的群聊ID"})
		return
	}
	var body struct {
		Title   string           `json:"title"`
		Content string           `json:"content"`
		Images  []map[string]any `json:"images"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if len([]rune(body.Content)) > int(config.MaxGroupAnnouncementLength) {
		c.JSON(400, gin.H{"error": "公告内容过长"})
		return
	}
	if int64(len(body.Images)) > config.MaxGroupAnnouncementImages {
		c.JSON(400, gin.H{"error": "图片数量超过限制(最多9张)"})
		return
	}
	var imagesJSON datatypes.JSON
	if len(body.Images) > 0 {
		bs, _ := json.Marshal(body.Images)
		imagesJSON = datatypes.JSON(bs)
	}

	a, err := h.Svc.CreateGroupAnnouncement(tid, u.ID, body.Title, body.Content, imagesJSON)
	if err != nil {
		if err == service.ErrForbidden || err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[Group] Failed to create announcement for group %d: %v", tid, err)
			c.JSON(400, gin.H{"error": "创建公告失败"})
		}
		return
	}

	// 广播公告创建事件
	if h.Gw != nil {
		memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(tid)
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupAnnouncementCreate, gin.H{
			"threadId":     tid,
			"announcement": a,
		})
	}

	c.JSON(200, a)
}

// getGroupAnnouncement 获取单条群公告
func (h *HTTP) getGroupAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	announcementID, err := parseUintParam(c, "announcementId")
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的公告ID"})
		return
	}
	a, err := h.Svc.GetGroupAnnouncement(announcementID)
	if err != nil {
		c.JSON(404, gin.H{"error": "公告不存在"})
		return
	}
	// 验证成员身份
	if !h.Svc.Repo.IsGroupMember(a.ThreadID, u.ID) {
		c.JSON(403, gin.H{"error": "仅群成员可查看"})
		return
	}
	c.JSON(200, a)
}

// updateGroupAnnouncement 编辑群公告
func (h *HTTP) updateGroupAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	announcementID, err := parseUintParam(c, "announcementId")
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的公告ID"})
		return
	}
	var body struct {
		Title   *string           `json:"title"`
		Content *string           `json:"content"`
		Images  *[]map[string]any `json:"images"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if body.Content != nil && len([]rune(*body.Content)) > int(config.MaxGroupAnnouncementLength) {
		c.JSON(400, gin.H{"error": "公告内容过长"})
		return
	}
	var imagesPtr *datatypes.JSON
	if body.Images != nil {
		if int64(len(*body.Images)) > config.MaxGroupAnnouncementImages {
			c.JSON(400, gin.H{"error": "图片数量超过限制(最多9张)"})
			return
		}
		bs, _ := json.Marshal(*body.Images)
		imgJSON := datatypes.JSON(bs)
		imagesPtr = &imgJSON
	}

	a, err := h.Svc.UpdateGroupAnnouncement(announcementID, u.ID, body.Title, body.Content, imagesPtr)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "公告不存在"})
		} else if err == service.ErrForbidden || err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[Group] Failed to update announcement %d: %v", announcementID, err)
			c.JSON(500, gin.H{"error": "编辑公告失败"})
		}
		return
	}

	// 广播公告更新事件
	if h.Gw != nil {
		memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(a.ThreadID)
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupAnnouncementUpdate, gin.H{
			"threadId":     a.ThreadID,
			"announcement": a,
		})
	}

	c.JSON(200, a)
}

// deleteGroupAnnouncement 删除群公告
func (h *HTTP) deleteGroupAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	announcementID, err := parseUintParam(c, "announcementId")
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的公告ID"})
		return
	}

	// 先获取公告信息用于广播
	a, _ := h.Svc.GetGroupAnnouncement(announcementID)

	if err := h.Svc.DeleteGroupAnnouncement(announcementID, u.ID); err != nil {
		if err == service.ErrNotFound {
			c.Status(http.StatusNoContent)
			return
		} else if err == service.ErrForbidden || err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
			return
		}
		logger.Errorf("[Group] Failed to delete announcement %d: %v", announcementID, err)
		c.JSON(500, gin.H{"error": "删除公告失败"})
		return
	}

	// 广播公告删除事件
	if h.Gw != nil && a != nil {
		memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(a.ThreadID)
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupAnnouncementDelete, gin.H{
			"threadId":       a.ThreadID,
			"announcementId": announcementID,
		})
	}

	c.Status(http.StatusNoContent)
}

// pinGroupAnnouncement 置顶群公告
func (h *HTTP) pinGroupAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	announcementID, err := parseUintParam(c, "announcementId")
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的公告ID"})
		return
	}
	a, err := h.Svc.PinGroupAnnouncement(announcementID, u.ID)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "公告不存在"})
		} else if err == service.ErrForbidden || err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[Group] Failed to pin announcement %d: %v", announcementID, err)
			c.JSON(500, gin.H{"error": "置顶公告失败"})
		}
		return
	}
	c.JSON(200, a)
}

// unpinGroupAnnouncement 取消置顶群公告
func (h *HTTP) unpinGroupAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	announcementID, err := parseUintParam(c, "announcementId")
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的公告ID"})
		return
	}
	if err := h.Svc.UnpinGroupAnnouncement(announcementID, u.ID); err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "公告不存在"})
		} else if err == service.ErrForbidden || err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[Group] Failed to unpin announcement %d: %v", announcementID, err)
			c.JSON(500, gin.H{"error": "取消置顶失败"})
		}
		return
	}
	c.Status(http.StatusNoContent)
}

// getFeaturedGroupAnnouncement 获取精选群公告
func (h *HTTP) getFeaturedGroupAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的群聊ID"})
		return
	}
	a, err := h.Svc.GetFeaturedGroupAnnouncement(tid, u.ID)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "仅群成员可查看"})
		} else {
			c.JSON(404, gin.H{"error": "暂无公告"})
		}
		return
	}
	c.JSON(200, a)
}

// ==================== 群文件 ====================

// uploadGroupFile 上传群文件
func (h *HTTP) uploadGroupFile(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if tid == 0 {
		c.JSON(400, gin.H{"error": "无效的群聊ID"})
		return
	}

	if h.Svc.MinIO == nil {
		c.JSON(500, gin.H{"error": "文件存储服务不可用"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "缺少文件"})
		return
	}
	if file.Size > config.MaxGroupFileSize {
		c.JSON(400, gin.H{"error": "文件过大，最大100MB"})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(500, gin.H{"error": "打开文件失败"})
		return
	}
	defer src.Close()

	contentType := file.Header.Get("Content-Type")
	ctx := c.Request.Context()

	// 使用 group-files 分类存储
	objectName, errUpload := h.Svc.MinIO.UploadGuildFile(ctx, "group-files", uint(tid), src, file.Size, contentType)
	if errUpload != nil {
		logger.Errorf("[Group] Failed to upload file to MinIO for group %d: %v", tid, errUpload)
		c.JSON(500, gin.H{"error": "上传文件失败"})
		return
	}

	gf, err := h.Svc.UploadGroupFile(uint(tid), u.ID, file.Filename, objectName, contentType, file.Size)
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[Group] Failed to create group file record for group %d: %v", tid, err)
			c.JSON(500, gin.H{"error": "保存文件记录失败"})
		}
		return
	}

	gf.FileURL = h.Svc.MinIO.GetFileURL(objectName)
	c.JSON(200, gin.H{"data": gf})
}

// listGroupFiles 列出群文件
func (h *HTTP) listGroupFiles(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if tid == 0 {
		c.JSON(400, gin.H{"error": "无效的群聊ID"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	before, _ := strconv.ParseUint(c.Query("before"), 10, 64)

	files, err := h.Svc.ListGroupFiles(uint(tid), u.ID, limit, uint(before))
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "仅群成员可查看"})
		} else {
			logger.Errorf("[Group] Failed to list files for group %d: %v", tid, err)
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

// deleteGroupFile 删除群文件
func (h *HTTP) deleteGroupFile(c *gin.Context) {
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
	if err := h.Svc.DeleteGroupFile(uint(fileID), u.ID); err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "文件不存在"})
		} else if err == service.ErrForbidden || err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[Group] Failed to delete file %d: %v", fileID, err)
			c.JSON(500, gin.H{"error": "删除文件失败"})
		}
		return
	}
	c.Status(http.StatusNoContent)
}

// batchDeleteGroupFiles 批量删除群文件
// @Summary      Batch delete group files
// @Description  批量删除群文件（上传者、管理员或群主）
// @Tags         group-files
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "{fileIds: [uint]}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Router       /api/group/files/batch-delete [post]
func (h *HTTP) batchDeleteGroupFiles(c *gin.Context) {
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
		if err := h.Svc.DeleteGroupFile(fid, u.ID); err != nil {
			reason := "删除失败"
			if err == service.ErrNotFound {
				reason = "文件不存在"
			} else if err == service.ErrForbidden || err == service.ErrUnauthorized {
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
