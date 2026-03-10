package handlers

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/gin-gonic/gin"
)

// ==================== 隐藏会话管理 ====================

// setDmThreadHidden 设置/取消 DM 会话隐藏
func (h *HTTP) setDmThreadHidden(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	threadID, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		Hidden bool `json:"hidden"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.SetDmThreadHidden(threadID, u.ID, body.Hidden); err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "会话不存在"})
		} else {
			c.JSON(400, gin.H{"error": "操作失败"})
		}
		return
	}
	c.JSON(200, gin.H{"hidden": body.Hidden})
}

// listHiddenDmThreads 列出隐藏的 DM 会话
func (h *HTTP) listHiddenDmThreads(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	threads, total, err := h.Svc.ListHiddenDmThreads(u.ID, page, limit)
	if err != nil {
		c.JSON(500, gin.H{"error": "获取隐藏会话列表失败"})
		return
	}
	for i := range threads {
		if threads[i].Mentions == nil {
			threads[i].Mentions = make([]map[string]any, 0)
		}
	}
	c.JSON(200, gin.H{
		"data": threads,
		"pagination": gin.H{
			"page":    page,
			"limit":   limit,
			"total":   total,
			"hasMore": page*limit < total,
		},
	})
}

// setGroupThreadHidden 设置/取消群聊会话隐藏
func (h *HTTP) setGroupThreadHidden(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	threadID, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		Hidden bool `json:"hidden"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.SetGroupThreadHidden(threadID, u.ID, body.Hidden); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "非群成员"})
		} else {
			c.JSON(400, gin.H{"error": "操作失败"})
		}
		return
	}
	c.JSON(200, gin.H{"hidden": body.Hidden})
}

// listHiddenGroupThreads 列出隐藏的群聊会话
func (h *HTTP) listHiddenGroupThreads(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	beforeID64, _ := strconv.ParseUint(c.Query("beforeId"), 10, 64)
	afterID64, _ := strconv.ParseUint(c.Query("afterId"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	threads, err := h.Svc.ListHiddenGroupThreads(u.ID, limit, uint(beforeID64), uint(afterID64))
	if err != nil {
		c.JSON(500, gin.H{"error": "获取隐藏群聊列表失败"})
		return
	}
	c.JSON(200, gin.H{"data": threads})
}

// setSubRoomHidden 设置/取消子房间会话隐藏
func (h *HTTP) setSubRoomHidden(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	roomID, err := parseUintParam(c, "roomId")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		Hidden bool `json:"hidden"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.SetSubRoomHidden(roomID, u.ID, body.Hidden); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "非子房间成员"})
		} else {
			c.JSON(400, gin.H{"error": "操作失败"})
		}
		return
	}
	c.JSON(200, gin.H{"hidden": body.Hidden})
}

// listHiddenSubRooms 列出隐藏的子房间会话
func (h *HTTP) listHiddenSubRooms(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	rooms, err := h.Svc.ListHiddenSubRooms(u.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": "获取隐藏子房间列表失败"})
		return
	}
	c.JSON(200, gin.H{"data": rooms})
}

// ==================== 群聊置顶管理 ====================

// pinGroupThread 设置/取消群聊置顶
func (h *HTTP) pinGroupThread(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	threadID, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		Pinned bool `json:"pinned"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.SetGroupThreadPinned(threadID, u.ID, body.Pinned); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "非群成员"})
		} else {
			c.JSON(400, gin.H{"error": "操作失败"})
		}
		return
	}
	c.JSON(200, gin.H{"pinned": body.Pinned})
}

// ==================== 统一会话查询 ====================

// listAllThreads 统一查询所有线程（私聊 + 群聊）
// 支持参数:
//   - type: dm / group / 空(全部)
//   - filter: 逗号分隔的组合值，可选 common / hidden / pinned
//     不传或 all → 返回全部（common + hidden + pinned）
//     common    → 仅普通会话（非隐藏、非置顶）
//     hidden    → 仅隐藏会话
//     pinned    → 仅置顶会话
//     common,pinned → 普通 + 置顶（不含隐藏）
//     hidden,pinned → 隐藏 + 置顶（不含普通）
//     common,hidden → 普通 + 隐藏（不含置顶）
//     common,hidden,pinned → 全部
//   - limit: 每页数量
//   - beforeId / afterId: 游标分页
func (h *HTTP) listAllThreads(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	threadType := c.Query("type") // dm / group / 空
	filterRaw := c.Query("filter")
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	beforeID64, _ := strconv.ParseUint(c.Query("beforeId"), 10, 64)
	afterID64, _ := strconv.ParseUint(c.Query("afterId"), 10, 64)
	beforeID := uint(beforeID64)
	afterID := uint(afterID64)

	// 解析 filter 组合
	wantCommon := false
	wantHidden := false
	wantPinned := false
	if filterRaw == "" || filterRaw == "all" {
		// 默认 = all
		wantCommon = true
		wantHidden = true
		wantPinned = true
	} else {
		for _, f := range strings.Split(filterRaw, ",") {
			switch strings.TrimSpace(f) {
			case "common":
				wantCommon = true
			case "hidden":
				wantHidden = true
			case "pinned":
				wantPinned = true
			}
		}
	}

	type threadItem struct {
		Type        string      `json:"type"`              // dm / group
		ID          uint        `json:"id"`                // 线程ID
		IsPinned    bool        `json:"isPinned"`          // 是否置顶
		IsHidden    bool        `json:"isHidden"`          // 是否隐藏
		IsMuted     bool        `json:"isMuted,omitempty"` // 是否免打扰
		LastMsgTime *time.Time  `json:"lastMessageAt,omitempty"`
		DmThread    interface{} `json:"dmThread,omitempty"`    // 私聊线程详情
		GroupThread interface{} `json:"groupThread,omitempty"` // 群聊线程详情
	}

	// classify 判断线程属于哪类，并决定是否包含
	shouldInclude := func(isPinned, isHidden bool) bool {
		if isHidden {
			return wantHidden
		}
		if isPinned {
			return wantPinned
		}
		return wantCommon
	}

	var items []threadItem
	seen := make(map[string]bool) // 去重 key = "dm:123" / "group:456"

	addItem := func(item threadItem) {
		key := item.Type + ":" + strconv.FormatUint(uint64(item.ID), 10)
		if !seen[key] {
			seen[key] = true
			items = append(items, item)
		}
	}

	// ---- 查询 DM 线程 ----
	if threadType == "" || threadType == "dm" {
		// 隐藏会话
		if wantHidden {
			threads, _, err := h.Svc.ListHiddenDmThreads(u.ID, 1, limit)
			if err == nil {
				for _, t := range threads {
					if shouldInclude(t.IsPinned, true) {
						addItem(threadItem{
							Type: "dm", ID: t.ID,
							IsPinned: t.IsPinned, IsHidden: true,
							LastMsgTime: t.LastMessageAt, DmThread: t,
						})
					}
				}
			}
		}
		// 置顶会话
		if wantPinned {
			threads, err := h.Svc.ListPinnedDmThreads(u.ID)
			if err == nil {
				for _, t := range threads {
					if shouldInclude(true, t.IsHidden) {
						addItem(threadItem{
							Type: "dm", ID: t.ID,
							IsPinned: true, IsHidden: t.IsHidden,
							LastMsgTime: t.LastMessageAt, DmThread: t,
						})
					}
				}
			}
		}
		// 普通会话
		if wantCommon {
			threads, err := h.Svc.ListDmThreadsCursor(u.ID, limit, beforeID, afterID)
			if err == nil {
				for i := range threads {
					if threads[i].Mentions == nil {
						threads[i].Mentions = make([]map[string]any, 0)
					}
				}
				for _, t := range threads {
					if shouldInclude(t.IsPinned, false) {
						addItem(threadItem{
							Type: "dm", ID: t.ID,
							IsPinned: t.IsPinned, IsHidden: false,
							LastMsgTime: t.LastMessageAt, DmThread: t,
						})
					}
				}
			}
		}
	}

	// ---- 查询群聊线程 ----
	if threadType == "" || threadType == "group" {
		if wantHidden {
			threads, err := h.Svc.ListHiddenGroupThreads(u.ID, limit, beforeID, afterID)
			if err == nil {
				for _, t := range threads {
					if shouldInclude(t.IsPinned, true) {
						addItem(threadItem{
							Type: "group", ID: t.ID,
							IsPinned: t.IsPinned, IsHidden: true, IsMuted: t.IsMuted,
							LastMsgTime: t.LastMessageAt, GroupThread: t,
						})
					}
				}
			}
		}
		if wantPinned {
			threads, err := h.Svc.ListPinnedGroupThreads(u.ID)
			if err == nil {
				for _, t := range threads {
					if shouldInclude(true, t.IsHidden) {
						addItem(threadItem{
							Type: "group", ID: t.ID,
							IsPinned: true, IsHidden: t.IsHidden, IsMuted: t.IsMuted,
							LastMsgTime: t.LastMessageAt, GroupThread: t,
						})
					}
				}
			}
		}
		if wantCommon {
			threads, err := h.Svc.ListUserGroupThreads(u.ID, limit, beforeID, afterID)
			if err == nil {
				for _, t := range threads {
					if shouldInclude(t.IsPinned, false) {
						addItem(threadItem{
							Type: "group", ID: t.ID,
							IsPinned: t.IsPinned, IsHidden: false, IsMuted: t.IsMuted,
							LastMsgTime: t.LastMessageAt, GroupThread: t,
						})
					}
				}
			}
		}
	}

	// 排序：置顶优先 → lastMessageAt 倒序
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].IsPinned != items[j].IsPinned {
			return items[i].IsPinned
		}
		ti := items[i].LastMsgTime
		tj := items[j].LastMsgTime
		if ti == nil && tj == nil {
			return items[i].ID > items[j].ID
		}
		if ti == nil {
			return false
		}
		if tj == nil {
			return true
		}
		return ti.After(*tj)
	})

	if items == nil {
		items = make([]threadItem, 0)
	}

	c.JSON(200, gin.H{"data": items})
}

// ==================== 统一精华消息查询 ====================

// listAllPinnedMessages 统一查询所有精华/置顶消息
// 支持参数:
//   - type: channel / dm / group / 空(全部)
func (h *HTTP) listAllPinnedMessages(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	msgType := c.Query("type") // channel / dm / group / 空

	type pinnedItem struct {
		Type     string      `json:"type"`     // channel / dm / group
		SourceID uint        `json:"sourceId"` // channelId / threadId / groupThreadId
		Message  interface{} `json:"message"`  // 消息内容
		PinnedAt time.Time   `json:"pinnedAt"` // 置顶时间
	}

	var items []pinnedItem

	// ---- 频道精华消息 ----
	if msgType == "" || msgType == "channel" {
		// 获取用户所在的所有公会的频道
		guilds, err := h.Svc.ListUserGuilds(u.ID)
		if err == nil {
			for _, g := range guilds {
				channels, err := h.Svc.Repo.ListChannels(g.ID)
				if err != nil {
					continue
				}
				for _, ch := range channels {
					pinnedList, err := h.Svc.Repo.ListPinnedMessages(ch.ID)
					if err != nil {
						continue
					}
					for _, pm := range pinnedList {
						msg, err := h.Svc.Repo.GetMessage(pm.MessageID)
						if err != nil || msg.DeletedAt != nil {
							continue
						}
						items = append(items, pinnedItem{
							Type:     "channel",
							SourceID: ch.ID,
							Message:  msg,
							PinnedAt: pm.CreatedAt,
						})
					}
				}
			}
		}
	}

	// ---- DM 精华消息 ----
	if msgType == "" || msgType == "dm" {
		dmThreads, err := h.Svc.ListDmThreadsCursor(u.ID, 200, 0, 0)
		if err == nil {
			for _, t := range dmThreads {
				pinnedList, err := h.Svc.Repo.ListPinnedMessagesByThread(t.ID)
				if err != nil {
					continue
				}
				for _, pm := range pinnedList {
					msg, err := h.Svc.Repo.GetDmMessage(pm.MessageID)
					if err != nil || msg.DeletedAt != nil {
						continue
					}
					items = append(items, pinnedItem{
						Type:     "dm",
						SourceID: t.ID,
						Message:  msg,
						PinnedAt: pm.CreatedAt,
					})
				}
			}
		}
	}

	// ---- 群聊精华消息 ----
	if msgType == "" || msgType == "group" {
		groupThreads, err := h.Svc.ListUserGroupThreads(u.ID, 200, 0, 0)
		if err == nil {
			for _, t := range groupThreads {
				pinnedList, err := h.Svc.Repo.ListPinnedGroupMessages(t.ID)
				if err != nil {
					continue
				}
				for _, pm := range pinnedList {
					if pm.GroupMessageID == nil {
						continue
					}
					msg, err := h.Svc.Repo.GetGroupMessage(*pm.GroupMessageID)
					if err != nil || msg.DeletedAt != nil {
						continue
					}
					items = append(items, pinnedItem{
						Type:     "group",
						SourceID: t.ID,
						Message:  msg,
						PinnedAt: pm.CreatedAt,
					})
				}
			}
		}
	}

	// 按置顶时间倒序排序
	sort.Slice(items, func(i, j int) bool {
		return items[i].PinnedAt.After(items[j].PinnedAt)
	})

	if items == nil {
		items = make([]pinnedItem, 0)
	}

	c.JSON(200, gin.H{"data": items})
}
