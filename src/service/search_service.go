package service

import (
	"bubble/src/db/models"
)

// SearchMessageResult 全局搜索的消息结果
type SearchMessageResult struct {
	ID           uint   `json:"id"`
	Content      string `json:"content"`
	AuthorID     uint   `json:"authorId"`
	AuthorName   string `json:"authorName"`
	AuthorAvatar string `json:"authorAvatar"`
	ThreadType   string `json:"threadType"` // "dm" or "group"
	ThreadID     uint   `json:"threadId"`
	ThreadName   string `json:"threadName"`
	CreatedAt    string `json:"createdAt"`
}

// SearchUserGroupThreads 搜索用户参与的群聊（支持按名称和ID搜索）
func (s *Service) SearchUserGroupThreads(userID uint, q string, limit int) ([]models.GroupThread, error) {
	threads, err := s.Repo.SearchUserGroupThreads(userID, q, limit)
	if err != nil {
		return nil, err
	}
	for i := range threads {
		cnt, _ := s.Repo.CountGroupMembers(threads[i].ID)
		threads[i].MemberCount = int(cnt)
	}
	return threads, nil
}

// GlobalSearchMessages 搜索用户可见的所有聊天记录（DM + 群聊）
func (s *Service) GlobalSearchMessages(userID uint, q string, limit int) ([]SearchMessageResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	half := limit / 2
	if half < 5 {
		half = 5
	}

	var results []SearchMessageResult

	// 搜索私聊消息
	dmMsgs, err := s.Repo.SearchDmMessages(userID, q, half)
	if err == nil {
		// 批量收集需要的用户ID和线程ID
		threadMap := make(map[uint]*models.DmThread)
		userMap := make(map[uint]*models.User)

		for _, msg := range dmMsgs {
			if _, ok := threadMap[msg.ThreadID]; !ok {
				if t, err := s.Repo.GetDmThread(msg.ThreadID); err == nil {
					threadMap[msg.ThreadID] = t
				}
			}
			if _, ok := userMap[msg.AuthorID]; !ok {
				if u, err := s.GetUserByID(msg.AuthorID); err == nil {
					userMap[msg.AuthorID] = u
				}
			}
		}

		for _, msg := range dmMsgs {
			r := SearchMessageResult{
				ID:         msg.ID,
				Content:    msg.Content,
				AuthorID:   msg.AuthorID,
				ThreadType: "dm",
				ThreadID:   msg.ThreadID,
				CreatedAt:  msg.CreatedAt.Format("2006-01-02T15:04:05Z"),
			}
			if u, ok := userMap[msg.AuthorID]; ok {
				r.AuthorName = u.Name
				r.AuthorAvatar = u.Avatar
			}
			if t, ok := threadMap[msg.ThreadID]; ok {
				// DM 线程名 = 对方用户名
				peerID := t.UserBID
				if t.UserBID == userID {
					peerID = t.UserAID
				}
				if peer, ok := userMap[peerID]; ok {
					r.ThreadName = peer.Name
				} else if peer, err := s.GetUserByID(peerID); err == nil {
					r.ThreadName = peer.Name
					userMap[peerID] = peer
				}
			}
			results = append(results, r)
		}
	}

	// 搜索群聊消息
	groupMsgs, err := s.Repo.SearchGroupMessages(userID, q, half)
	if err == nil {
		threadMap := make(map[uint]*models.GroupThread)
		userMap := make(map[uint]*models.User)

		for _, msg := range groupMsgs {
			if _, ok := threadMap[msg.ThreadID]; !ok {
				if t, err := s.Repo.GetGroupThread(msg.ThreadID); err == nil {
					threadMap[msg.ThreadID] = t
				}
			}
			if _, ok := userMap[msg.AuthorID]; !ok {
				if u, err := s.GetUserByID(msg.AuthorID); err == nil {
					userMap[msg.AuthorID] = u
				}
			}
		}

		for _, msg := range groupMsgs {
			r := SearchMessageResult{
				ID:         msg.ID,
				Content:    msg.Content,
				AuthorID:   msg.AuthorID,
				ThreadType: "group",
				ThreadID:   msg.ThreadID,
				CreatedAt:  msg.CreatedAt.Format("2006-01-02T15:04:05Z"),
			}
			if u, ok := userMap[msg.AuthorID]; ok {
				r.AuthorName = u.Name
				r.AuthorAvatar = u.Avatar
			}
			if t, ok := threadMap[msg.ThreadID]; ok {
				r.ThreadName = t.Name
			}
			results = append(results, r)
		}
	}

	return results, nil
}
