package handlers

import (
	"net/http"
	"time"

	"bubble/src/db/models"
	"bubble/src/middleware"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// ==================== Security & Admin ====================

// @Summary      Security status overview
// @Tags         security
// @Security     BearerAuth
// @Produce      json
// @Success      200 {object} map[string]any
// @Failure      401 {object} map[string]string
// @Failure      403 {object} map[string]string
// @Router       /api/security/status [get]
// securityStatus 返回系统安全相关统计与配置快照。
// Method/Path: GET /api/security/status
// 认证: 需要 Bearer Token。
// 权限: 仅系统管理员可访问(当前定义为用户 ID <= 2 的超级管理员账号)。
// 内容:
//
//	config: 登录与邮箱验证策略。
//	passwordPolicy: 当前固定密码策略参数(可未来配置化)。
//	users: 用户总数、验证与待验证计数。
//	requests: 最近一小时邮箱验证与密码重置请求数。
//	rateLimits: 验证与重置的时间间隔/小时上限(当前硬编码示例值)。
//	audit: 安全事件总数与最近事件时间。
//
// 用途: 后台管理面板或监控。
// 错误: 401 未认证; 403 非管理员; 500 数据库查询失败。
func (h *HTTP) securityStatus(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	// 仅允许系统管理员访问(ID <= 2 为超级管理员)
	if u.ID > 2 {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}
	// Aggregate counts
	var totalUsers, verifiedUsers, pendingVerify, recentVerifyReq, recentResetReq, totalEvents int64
	if err := h.Svc.Repo.DB.Model(&models.User{}).Count(&totalUsers).Error; err != nil {
		logger.Errorf("[Security] Failed to count total users: %v", err)
		c.JSON(500, gin.H{"error": "获取统计数据失败"})
		return
	}
	if err := h.Svc.Repo.DB.Model(&models.User{}).Where("email_verified = ?", true).Count(&verifiedUsers).Error; err != nil {
		logger.Errorf("[Security] Failed to count verified users: %v", err)
		c.JSON(500, gin.H{"error": "获取统计数据失败"})
		return
	}
	if err := h.Svc.Repo.DB.Model(&models.User{}).Where("email <> '' AND email_verified = ?", false).Count(&pendingVerify).Error; err != nil {
		logger.Errorf("[Security] Failed to count pending verify users: %v", err)
		c.JSON(500, gin.H{"error": "获取统计数据失败"})
		return
	}
	oneHourAgo := time.Now().Add(-1 * time.Hour)
	if err := h.Svc.Repo.DB.Model(&models.User{}).Where("email_verify_requested_at >= ?", oneHourAgo).Count(&recentVerifyReq).Error; err != nil {
		logger.Errorf("[Security] Failed to count recent verify requests: %v", err)
		c.JSON(500, gin.H{"error": "获取统计数据失败"})
		return
	}
	if err := h.Svc.Repo.DB.Model(&models.User{}).Where("reset_requested_at >= ?", oneHourAgo).Count(&recentResetReq).Error; err != nil {
		logger.Errorf("[Security] Failed to count recent reset requests: %v", err)
		c.JSON(500, gin.H{"error": "获取统计数据失败"})
		return
	}
	if err := h.Svc.Repo.DB.Model(&models.SecurityEvent{}).Count(&totalEvents).Error; err != nil {
		logger.Errorf("[Security] Failed to count security events: %v", err)
		c.JSON(500, gin.H{"error": "获取统计数据失败"})
		return
	}
	var lastEv models.SecurityEvent
	var lastEventAt string
	if err := h.Svc.Repo.DB.Order("created_at desc").First(&lastEv).Error; err == nil {
		lastEventAt = lastEv.CreatedAt.UTC().Format(time.RFC3339)
	}

	// Rate limit policy (mirrors service logic constants)
	verifyIntervalSeconds := 60
	verifyMaxPerHour := 5
	resetIntervalSeconds := 60
	resetMaxPerHour := 5

	c.JSON(200, gin.H{
		"config": gin.H{
			"allowAnonymousLogin":  h.Cfg != nil && h.Cfg.AllowAnonymousLogin,
			"requireEmailVerified": h.Cfg != nil && h.Cfg.RequireEmailVerified,
		},
		"passwordPolicy": gin.H{
			"minLength":       8,
			"requiresLetters": true,
			"requiresDigits":  true,
		},
		"users": gin.H{
			"total":         totalUsers,
			"verified":      verifiedUsers,
			"pendingVerify": pendingVerify,
		},
		"requests": gin.H{
			"emailVerifyLastHour":   recentVerifyReq,
			"passwordResetLastHour": recentResetReq,
		},
		"rateLimits": gin.H{
			"verifyIntervalSeconds": verifyIntervalSeconds,
			"verifyMaxPerHour":      verifyMaxPerHour,
			"resetIntervalSeconds":  resetIntervalSeconds,
			"resetMaxPerHour":       resetMaxPerHour,
		},
		"audit": gin.H{
			"totalEvents": totalEvents,
			"lastEventAt": lastEventAt,
		},
	})
}
