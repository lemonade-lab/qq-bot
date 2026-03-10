package service

import (
	"bubble/src/db/models"
	"strconv"
	"strings"
)

// Friends

func (s *Service) SendFriendRequest(fromID uint, toName string) (*models.User, *models.UserNotification, error) {
	return s.SendFriendRequestWithAnswer(fromID, toName, "")
}

// SendFriendRequestWithAnswer 发送好友请求（支持回答验证问题）
func (s *Service) SendFriendRequestWithAnswer(fromID uint, toName, answer string) (*models.User, *models.UserNotification, error) {
	if toName == "" {
		return nil, nil, ErrBadRequest
	}
	to, err := s.Repo.GetUserByName(toName)
	if err != nil || to == nil {
		return nil, nil, ErrNotFound
	}
	if fromID == to.ID {
		return nil, nil, &Err{Code: 400, Msg: "不能添加自己为好友"}
	}
	// 检查是否被对方拉黑（对方无感知，返回通用错误）
	blocked, err := s.Repo.IsBlocked(to.ID, fromID)
	if err != nil {
		return nil, nil, err
	}
	if blocked {
		// 返回通用错误，不让发送者知道被拉黑了
		return nil, nil, &Err{Code: 400, Msg: "发送好友申请失败"}
	}
	if ok, status, err := s.Repo.ExistsFriendship(fromID, to.ID); err != nil {
		return nil, nil, err
	} else if ok {
		if status == "accepted" {
			// 已经是好友，返回成功状态码但提示已是好友
			return nil, nil, &Err{Code: 200, Msg: "已是好友"}
		}
		// 申请已存在，返回成功避免重复发送
		return nil, nil, &Err{Code: 200, Msg: "好友申请已发送"}
	}

	// 获取对方的好友验证模式
	mode := strings.TrimSpace(to.FriendRequestMode)
	if mode == "" {
		// 兼容旧数据：如果没有设置mode，使用RequireFriendApproval判断
		if to.RequireFriendApproval {
			mode = "need_approval"
		} else {
			mode = "everyone"
		}
	}

	switch mode {
	case "everyone":
		// 允许任何人，直接建立好友关系
		return to, nil, s.Repo.CreateAcceptedFriendship(fromID, to.ID)

	case "need_question":
		// 需要回答问题
		if strings.TrimSpace(to.FriendVerifyAnswer) == "" {
			// 对方未设置验证答案，降级为need_approval模式
			mode = "need_approval"
			break
		}
		// 检查答案是否正确（忽略大小写和首尾空格）
		if strings.EqualFold(strings.TrimSpace(answer), strings.TrimSpace(to.FriendVerifyAnswer)) {
			// 答案正确，直接建立好友关系
			return to, nil, s.Repo.CreateAcceptedFriendship(fromID, to.ID)
		} else {
			// 答案错误或未提供答案
			if answer == "" {
				// 未提供答案，返回需要回答问题的提示
				return to, nil, &Err{Code: 400, Msg: "需要回答验证问题", Data: map[string]string{"question": to.FriendVerifyQuestion}}
			}
			// 答案错误
			return nil, nil, &Err{Code: 400, Msg: "验证答案错误"}
		}

	case "need_approval":
		fallthrough
	default:
		// 需要验证（默认模式）
	}

	// 创建pending请求
	if err := s.Repo.CreateFriendRequest(fromID, to.ID); err != nil {
		return nil, nil, err
	}

	// 同时创建好友申请通知
	notif, err := s.Repo.CreateFriendRequestNotification(to.ID, fromID)
	if err != nil {
		return to, nil, err
	}

	return to, notif, nil
}

func (s *Service) AcceptFriendRequest(meID uint, fromName string) error {
	from, err := s.Repo.GetUserByName(fromName)
	if err != nil || from == nil {
		return ErrNotFound
	}
	if ok, status, err := s.Repo.ExistsFriendship(from.ID, meID); err != nil {
		return err
	} else if !ok || status != "pending" {
		return ErrBadRequest
	}
	return s.Repo.AcceptFriendRequest(from.ID, meID)
}

func (s *Service) RemoveFriend(a, b uint) error { return s.Repo.DeleteFriendship(a, b) }

// SetFriendRequestMode 设置好友验证模式
// mode: need_approval(需要验证) | everyone(允许任何人) | need_question(回答问题后验证)
func (s *Service) SetFriendRequestMode(userID uint, mode, question, answer string) error {
	// 验证模式
	mode = strings.TrimSpace(mode)
	if mode != "need_approval" && mode != "everyone" && mode != "need_question" {
		return &Err{Code: 400, Msg: "无效的验证模式"}
	}

	// 如果是need_question模式，必须提供问题和答案
	if mode == "need_question" {
		question = strings.TrimSpace(question)
		answer = strings.TrimSpace(answer)
		if question == "" || answer == "" {
			return &Err{Code: 400, Msg: "需要提供验证问题和答案"}
		}
		if len([]rune(question)) > 256 || len([]rune(answer)) > 256 {
			return &Err{Code: 400, Msg: "问题或答案过长（最多256字符）"}
		}
	}

	updates := map[string]interface{}{
		"friend_request_mode": mode,
	}

	if mode == "need_question" {
		updates["friend_verify_question"] = question
		updates["friend_verify_answer"] = answer
	} else {
		// 清空验证问题和答案
		updates["friend_verify_question"] = ""
		updates["friend_verify_answer"] = ""
	}

	return s.Repo.DB.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error
}

// NOTE: ListFriends and ListFriendRequests with pagination are now in pagination_service.go

// SetFriendPrivacyMode 设置对特定好友的隐私模式
// mode: normal（默认）| chat_only（仅聊天，对方看不到我的朋友圈）
func (s *Service) SetFriendPrivacyMode(userID, friendID uint, mode string) error {
	// 验证模式
	if mode != "normal" && mode != "chat_only" {
		return &Err{Code: 400, Msg: "无效的隐私模式"}
	}

	// 查找好友关系
	var friendship models.Friendship
	err := s.Repo.DB.Where(
		"((from_user_id = ? AND to_user_id = ?) OR (from_user_id = ? AND to_user_id = ?)) AND status = 'accepted'",
		userID, friendID, friendID, userID,
	).First(&friendship).Error

	if err != nil {
		return ErrNotFound
	}

	// 更新隐私模式
	if friendship.FromUserID == userID {
		// 当前用户是FromUser，更新PrivacyModeFrom
		return s.Repo.DB.Model(&friendship).Update("privacy_mode_from", mode).Error
	} else {
		// 当前用户是ToUser，更新PrivacyModeTo
		return s.Repo.DB.Model(&friendship).Update("privacy_mode_to", mode).Error
	}
}

func (s *Service) SetFriendNickname(userID, friendID uint, nickname string) error {
	nickname = strings.TrimSpace(nickname)
	if len(nickname) > 64 {
		return &Err{Code: 400, Msg: "备注名过长"}
	}
	// 验证是否是好友关系
	ok, status, err := s.Repo.ExistsFriendship(userID, friendID)
	if err != nil {
		return err
	}
	if !ok || status != "accepted" {
		return &Err{Code: 400, Msg: "你们还不是好友"}
	}
	return s.Repo.SetFriendNickname(userID, friendID, nickname)
}

// Blacklist 黑名单相关
// SetDmPrivacyMode 设置用户的私聊隐私模式
// mode: friends_only(仅好友可创建私聊) | everyone(所有人都可以)
func (s *Service) SetDmPrivacyMode(userID uint, mode string) error {
	// 验证模式
	if mode != "friends_only" && mode != "everyone" {
		return &Err{Code: 400, Msg: "无效的隐私模式"}
	}

	return s.Repo.DB.Model(&models.User{}).Where("id = ?", userID).Update("dm_privacy_mode", mode).Error
}

func (s *Service) AddToBlacklist(userID, blockedID uint) error {
	if userID == blockedID {
		return &Err{Code: 400, Msg: "不能拉黑自己"}
	}
	// 检查被拉黑的用户是否存在
	blocked, err := s.Repo.GetUserByID(blockedID)
	if err != nil || blocked == nil {
		return ErrNotFound
	}
	return s.Repo.AddToBlacklist(userID, blockedID)
}

func (s *Service) RemoveFromBlacklist(userID, blockedID uint) error {
	return s.Repo.RemoveFromBlacklist(userID, blockedID)
}

func (s *Service) ListBlacklist(userID uint) ([]models.Blacklist, error) {
	return s.Repo.ListBlacklist(userID)
}

// GreetUser 向指定用户打招呼，只能打一次
func (s *Service) GreetUser(fromUserID, toUserID uint) error {
	if fromUserID == toUserID {
		return &Err{Code: 400, Msg: "不能向自己打招呼"}
	}
	// 检查目标用户是否存在
	to, err := s.Repo.GetUserByID(toUserID)
	if err != nil || to == nil {
		return ErrNotFound
	}
	// 检查是否被对方拉黑（对方无感知，返回通用错误）
	blocked, err := s.Repo.IsBlocked(toUserID, fromUserID)
	if err != nil {
		return err
	}
	if blocked {
		// 返回通用错误，不让发送者知道被拉黑了
		return &Err{Code: 400, Msg: "打招呼失败"}
	}
	// 检查是否已经打过招呼
	hasGreeted, err := s.Repo.HasGreeted(fromUserID, toUserID)
	if err != nil {
		return err
	}
	if hasGreeted {
		return &Err{Code: 400, Msg: "已打过招呼"}
	}
	// 创建打招呼记录
	return s.Repo.CreateGreeting(fromUserID, toUserID)
}

// AreFriends 检查两个用户是否是好友
func (s *Service) AreFriends(userID1, userID2 uint) bool {
	status, _ := s.Repo.GetFriendshipStatus(userID1, userID2)
	return status == "accepted"
}

// SearchFriends 搜索好友（支持ID和名称搜索）
func (s *Service) SearchFriends(userID uint, q string, limit int) ([]models.User, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, ErrBadRequest
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	// 支持ID搜索
	if id, err := strconv.ParseUint(q, 10, 32); err == nil {
		// 检查是否是好友
		if s.AreFriends(userID, uint(id)) {
			user, err := s.Repo.GetUserByID(uint(id))
			if err == nil && user != nil {
				return []models.User{*user}, nil
			}
		}
		return []models.User{}, nil
	}
	// 名称/备注模糊搜索
	return s.Repo.SearchFriends(userID, q, limit)
}

// AcceptFriendRequestNotification 通过通知接受好友申请
func (s *Service) AcceptFriendRequestNotification(userID uint, notificationID uint) error {
	// 获取通知
	notif, err := s.Repo.GetNotificationByID(notificationID, userID)
	if err != nil {
		return &Err{Code: 404, Msg: "通知不存在"}
	}

	// 验证通知类型
	if notif.Type != "friend_request" {
		return &Err{Code: 400, Msg: "通知类型错误"}
	}

	// 验证状态
	if notif.Status == nil || *notif.Status != "pending" {
		return &Err{Code: 400, Msg: "申请已处理"}
	}

	// 验证申请人
	if notif.AuthorID == nil {
		return &Err{Code: 400, Msg: "申请人信息缺失"}
	}

	fromUserID := *notif.AuthorID

	// 检查好友关系是否已存在
	if ok, status, err := s.Repo.ExistsFriendship(fromUserID, userID); err != nil {
		return err
	} else if !ok || status != "pending" {
		// 更新通知状态为已接受（即使关系不存在或已改变）
		_ = s.Repo.UpdateNotificationStatus(notificationID, userID, "accepted")
		return &Err{Code: 400, Msg: "好友申请不存在或已处理"}
	}

	// 接受好友申请
	if err := s.Repo.AcceptFriendRequest(fromUserID, userID); err != nil {
		return err
	}

	// 更新通知状态
	return s.Repo.UpdateNotificationStatus(notificationID, userID, "accepted")
}

// RejectFriendRequestNotification 通过通知拒绝好友申请
func (s *Service) RejectFriendRequestNotification(userID uint, notificationID uint) error {
	// 获取通知
	notif, err := s.Repo.GetNotificationByID(notificationID, userID)
	if err != nil {
		return &Err{Code: 404, Msg: "通知不存在"}
	}

	// 验证通知类型
	if notif.Type != "friend_request" {
		return &Err{Code: 400, Msg: "通知类型错误"}
	}

	// 验证状态
	if notif.Status == nil || *notif.Status != "pending" {
		return &Err{Code: 400, Msg: "申请已处理"}
	}

	// 验证申请人
	if notif.AuthorID == nil {
		return &Err{Code: 400, Msg: "申请人信息缺失"}
	}

	fromUserID := *notif.AuthorID

	// 拒绝好友申请（删除好友关系记录）
	if err := s.Repo.DeleteFriendship(fromUserID, userID); err != nil {
		// 即使删除失败也更新通知状态
		_ = s.Repo.UpdateNotificationStatus(notificationID, userID, "rejected")
		return err
	}

	// 更新通知状态
	return s.Repo.UpdateNotificationStatus(notificationID, userID, "rejected")
}
