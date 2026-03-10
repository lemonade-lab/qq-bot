package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"bubble/src/config"
	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      Get all available permissions
// @Tags         permissions
// @Produce      json
// @Success      200  {array}  service.PermissionInfo
// @Router       /api/permissions [get]
// getAllPermissions 返回权限枚举
func (h *HTTP) getAllPermissions(c *gin.Context) {
	c.JSON(200, service.GetAllPermissions())
}

// @Summary      List roles
// @Tags         roles
// @Security     BearerAuth
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Success      200  {array}  map[string]any
// @Router       /api/guilds/{id}/roles [get]
func (h *HTTP) listRoles(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	roles, err := h.Svc.ListGuildRoles(uint(gid64), u.ID)
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[Roles] Failed to list roles in guild %d: %v", gid64, err)
			c.JSON(500, gin.H{"error": "获取角色列表失败"})
		}
		return
	}
	c.JSON(200, roles)
}

// @Summary      Create role
// @Tags         roles
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Param        body body  map[string]any true "{name, permissions(uint64)}"
// @Success      200  {object}  map[string]any
// @Failure      403  {object}  map[string]string
// @Router       /api/guilds/{id}/roles [post]
// createRole 为服务器创建新角色
func (h *HTTP) createRole(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var body struct {
		Name        string `json:"name"`
		Permissions uint64 `json:"permissions"`
		Color       string `json:"color"`
	}
	if err := c.BindJSON(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	// 角色名长度校验
	if l := len([]rune(strings.TrimSpace(body.Name))); l == 0 || l > int(config.MaxRoleNameLength) {
		c.JSON(400, gin.H{"error": "角色名称长度不合法"})
		return
	}
	role, err := h.Svc.CreateRole(uint(gid64), u.ID, body.Name, body.Permissions, body.Color)
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[Roles] Failed to create role in guild %d: %v", gid64, err)
			c.JSON(400, gin.H{"error": "创建角色失败"})
		}
		return
	}
	c.JSON(200, role)
}

// @Summary      Update role
// @Tags         roles
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id     path  int  true  "Guild ID"
// @Param        roleId path  int  true  "Role ID"
// @Param        body   body  map[string]any true "{name?, permissions?}"
// @Success      200    {object}  models.Role
// @Failure      403    {object}  map[string]string
// @Router       /api/guilds/{id}/roles/{roleId} [put]
// updateRole 更新角色
func (h *HTTP) updateRole(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	rid64, _ := strconv.ParseUint(c.Param("roleId"), 10, 64)
	var body struct {
		Name        *string `json:"name"`
		Permissions *uint64 `json:"permissions"`
		Color       *string `json:"color"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if body.Name != nil {
		if l := len([]rune(strings.TrimSpace(*body.Name))); l == 0 || l > int(config.MaxRoleNameLength) {
			c.JSON(400, gin.H{"error": "角色名称长度不合法"})
			return
		}
	}
	role, err := h.Svc.UpdateRole(uint(gid64), u.ID, uint(rid64), body.Name, body.Permissions, body.Color)
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[Roles] Failed to update role %d in guild %d: %v", rid64, gid64, err)
			c.JSON(400, gin.H{"error": "更新角色失败"})
		}
		return
	}
	c.JSON(200, role)
}

// @Summary      Delete role
// @Tags         roles
// @Security     BearerAuth
// @Produce      json
// @Param        id     path  int  true  "Guild ID"
// @Param        roleId path  int  true  "Role ID"
// @Success      204    {string} string ""
// @Failure      403    {object}  map[string]string
// @Router       /api/guilds/{id}/roles/{roleId} [delete]
// deleteRole 删除角色
func (h *HTTP) deleteRole(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	rid64, _ := strconv.ParseUint(c.Param("roleId"), 10, 64)
	if err := h.Svc.DeleteRole(uint(gid64), u.ID, uint(rid64)); err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			// 如果角色不存在，也视为删除成功，避免重复点击报错
			if svcErr.Code == 404 {
				c.Status(204)
				return
			}
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[Roles] Failed to delete role %d in guild %d by user %d: %v", rid64, gid64, u.ID, err)
			c.JSON(400, gin.H{"error": "删除角色失败"})
		}
		return
	}
	c.Status(204)
}

// @Summary      Assign role
// @Tags         roles
// @Security     BearerAuth
// @Produce      json
// @Param        id     path  int  true  "Guild ID"
// @Param        roleId path  int  true  "Role ID"
// @Param        userId path  int  true  "User ID"
// @Success      204    {string} string ""
// @Router       /api/guilds/{id}/roles/{roleId}/assign/{userId} [post]
// assignRole 将角色赋予用户
func (h *HTTP) assignRole(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	rid64, _ := strconv.ParseUint(c.Param("roleId"), 10, 64)
	uid64, _ := strconv.ParseUint(c.Param("userId"), 10, 64)
	if err := h.Svc.AssignRoleToMember(uint(gid64), u.ID, uint(uid64), uint(rid64)); err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[Roles] Failed to assign role %d to user %d in guild %d: %v", rid64, uid64, gid64, err)
			c.JSON(400, gin.H{"error": "分配角色失败"})
		}
		return
	}
	// 广播成员角色更新事件
	h.Gw.BroadcastToGuild(uint(gid64), config.EventGuildMemberUpdate, gin.H{
		"userId":     uint(uid64),
		"operatorId": u.ID,
		"action":     "roles_added",
		"roleId":     uint(rid64),
	})
	c.Status(204)
}

// @Summary      Remove role
// @Tags         roles
// @Security     BearerAuth
// @Produce      json
// @Param        id     path  int  true  "Guild ID"
// @Param        roleId path  int  true  "Role ID"
// @Param        userId path  int  true  "User ID"
// @Success      204    {string} string ""
// @Router       /api/guilds/{id}/roles/{roleId}/remove/{userId} [post]
// removeRole 从用户移除角色
// @Summary      Remove role
// @Description  移除成员的角色（需要管理角色权限）
// @Tags         roles
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id        path  int  true  "Guild ID"
// @Param        memberId  path  int  true  "Member ID"
// @Param        roleId    path  int  true  "Role ID"
// @Success      200       {object}  map[string]string
// @Failure      400       {object}  map[string]string  "参数错误"
// @Failure      401       {object}  map[string]string  "未认证"
// @Failure      403       {object}  map[string]string  "权限不足"
// @Failure      404       {object}  map[string]string  "角色或成员不存在"
// @Failure      500       {object}  map[string]string  "服务器错误"
// @Router       /api/guilds/{id}/members/{memberId}/roles/{roleId} [delete]
func (h *HTTP) removeRole(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	rid64, _ := strconv.ParseUint(c.Param("roleId"), 10, 64)
	uid64, _ := strconv.ParseUint(c.Param("userId"), 10, 64)
	if err := h.Svc.RemoveRoleFromMember(uint(gid64), u.ID, uint(uid64), uint(rid64)); err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			// 如果角色或成员关系不存在，也视为移除成功
			if svcErr.Code == 404 {
				c.Status(204)
				return
			}
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[Roles] Failed to remove role %d from user %d in guild %d: %v", rid64, uid64, gid64, err)
			c.JSON(400, gin.H{"error": "移除角色失败"})
		}
		return
	}
	// 广播成员角色更新事件
	h.Gw.BroadcastToGuild(uint(gid64), config.EventGuildMemberUpdate, gin.H{
		"userId":     uint(uid64),
		"operatorId": u.ID,
		"action":     "roles_removed",
		"roleId":     uint(rid64),
	})
	c.Status(204)
}
