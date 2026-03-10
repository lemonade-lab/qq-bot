package service

import (
	"bubble/src/db/models"
	"bubble/src/repository"
	"fmt"
	"sort"
	"time"
)

// ==================== 机器人分类 ====================

// ListRobotCategories 获取所有机器人分类
func (s *Service) ListRobotCategories() ([]models.RobotCategory, error) {
	return s.Repo.ListRobotCategories()
}

// SetRobotCategory 设置机器人分类
func (s *Service) SetRobotCategory(robotID uint, category string) error {
	rb, err := s.Repo.GetRobotByID(robotID)
	if err != nil {
		return &Err{Code: 404, Msg: "机器人不存在"}
	}
	if category == "" {
		category = "其他"
	}
	// 验证分类是否有效
	categories, err := s.Repo.ListRobotCategories()
	if err != nil {
		return &Err{Code: 500, Msg: "获取分类列表失败"}
	}
	valid := false
	for _, c := range categories {
		if c.Name == category {
			valid = true
			break
		}
	}
	if !valid {
		return &Err{Code: 400, Msg: "无效的分类: " + category}
	}
	rb.Category = category
	return s.Repo.UpdateRobot(rb)
}

// ==================== 机器人排行榜（实时计算） ====================

// getHeatWeights 从配置中获取热度权重参数
func (s *Service) getHeatWeights() repository.HeatWeights {
	return repository.HeatWeights{
		Guild:       s.Cfg.RankWeightGuild,
		GuildGrowth: s.Cfg.RankWeightGuildGrowth,
		Message:     s.Cfg.RankWeightMessage,
		Interaction: s.Cfg.RankWeightInteraction,
	}
}

// applyDecay 对热度分数应用不活跃衰减
func (s *Service) applyDecay(rawScore, messageCount int64, lastActiveAt time.Time, periodEnd time.Time) (finalScore int64, decayed bool) {
	decayDays := s.Cfg.RankDecayDays
	decayPct := s.Cfg.RankDecayPercent
	if decayDays <= 0 || decayPct <= 0 {
		return rawScore, false
	}
	if messageCount == 0 && periodEnd.Sub(lastActiveAt).Hours() > float64(decayDays*24) {
		penalty := rawScore * int64(decayPct) / 100
		finalScore = rawScore - penalty
		if finalScore < 0 {
			finalScore = 0
		}
		return finalScore, true
	}
	return rawScore, false
}

// rankedItem 内部排行条目（含 Robot 信息用于过滤和返回）
type rankedItem struct {
	models.RobotRanking
	Robot *models.Robot `json:"robot"`
}

// isPeriodCompleted 判断指定周期是否已经结束（数据不会再变化）
func isPeriodCompleted(periodType, periodKey string) bool {
	now := time.Now()
	curDaily, curWeekly, curMonthly := repository.GetCurrentPeriodKeys(now)
	switch periodType {
	case "daily":
		return periodKey < curDaily
	case "weekly":
		return periodKey < curWeekly
	case "monthly":
		return periodKey < curMonthly
	}
	return false
}

// computeRankings 实时计算指定周期的完整排行榜（已排序、含 Robot 信息）
// 已结束周期缓存 24h，当前进行中周期按配置的短时长缓存
func (s *Service) computeRankings(periodType, periodKey string) ([]rankedItem, error) {
	cacheKey := fmt.Sprintf("robot_ranking_computed:%s:%s", periodType, periodKey)

	// 尝试读缓存（缓存的是完整排序列表，分页在内存做）
	type cachedList struct {
		Items []rankedItem `json:"items"`
	}
	if s.RedisClient != nil {
		var cached cachedList
		if err := s.getCache(cacheKey, &cached); err == nil && len(cached.Items) > 0 {
			return cached.Items, nil
		}
	}

	// 计算周期时间范围
	start, end, err := repository.GetPeriodTimeRange(periodType, periodKey)
	if err != nil {
		return nil, err
	}

	// 实时计算所有机器人热度
	results, err := s.Repo.CalcAllRobotHeatScores(start, end, s.getHeatWeights())
	if err != nil {
		return nil, err
	}

	// 批量加载所有 Robot + BotUser 信息（消除 N+1 查询）
	robotIDs := make([]uint, 0, len(results))
	for _, r := range results {
		robotIDs = append(robotIDs, r.RobotID)
	}
	robotMap, err := s.Repo.GetRobotsByIDs(robotIDs)
	if err != nil {
		return nil, err
	}

	items := make([]rankedItem, 0, len(results))
	for _, r := range results {
		robot, ok := robotMap[r.RobotID]
		if !ok {
			continue // 机器人被删除等异常情况，跳过
		}
		finalScore, decayed := s.applyDecay(r.HeatScore, r.MessageCount, r.LastActiveAt, end)
		items = append(items, rankedItem{
			RobotRanking: models.RobotRanking{
				RobotID:          r.RobotID,
				PeriodType:       periodType,
				PeriodKey:        periodKey,
				RawScore:         r.HeatScore,
				HeatScore:        finalScore,
				GuildCount:       r.GuildCount,
				GuildGrowth:      r.GuildGrowth,
				MessageCount:     r.MessageCount,
				InteractionCount: r.InteractionCount,
				DecayApplied:     decayed,
			},
			Robot: robot,
		})
	}

	// 按热度降序排序
	sort.Slice(items, func(i, j int) bool {
		if items[i].HeatScore != items[j].HeatScore {
			return items[i].HeatScore > items[j].HeatScore
		}
		return items[i].RobotID < items[j].RobotID
	})

	// 缓存策略：已结束周期 24h，进行中周期按维度短缓存
	if s.RedisClient != nil && len(items) > 0 {
		var cacheTTL time.Duration
		if isPeriodCompleted(periodType, periodKey) {
			// 历史周期数据已定型，长期缓存
			cacheTTL = 24 * time.Hour
		} else {
			// 当前进行中的周期，按维度短缓存
			cacheMin := s.Cfg.RankCacheDailyMin
			switch periodType {
			case "weekly":
				cacheMin = s.Cfg.RankCacheWeeklyMin
			case "monthly":
				cacheMin = s.Cfg.RankCacheMonthlyMin
			}
			if cacheMin <= 0 {
				cacheMin = 5
			}
			cacheTTL = time.Duration(cacheMin) * time.Minute
		}
		_ = s.setCache(cacheKey, cachedList{Items: items}, cacheTTL)
	}

	return items, nil
}

// GetRobotRankings 获取机器人排行榜（实时计算 + 内存过滤分页）
func (s *Service) GetRobotRankings(periodType, periodKey, category string, limit, offset int) ([]models.RobotRanking, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// 默认当前周期
	now := time.Now()
	if periodKey == "" {
		dailyKey, weeklyKey, monthlyKey := repository.GetCurrentPeriodKeys(now)
		switch periodType {
		case "daily":
			periodKey = dailyKey
		case "weekly":
			periodKey = weeklyKey
		case "monthly":
			periodKey = monthlyKey
		default:
			periodType = "daily"
			periodKey = dailyKey
		}
	}

	allItems, err := s.computeRankings(periodType, periodKey)
	if err != nil {
		return nil, 0, err
	}

	// 过滤：仅公开 + 分类
	filtered := make([]rankedItem, 0, len(allItems))
	for _, item := range allItems {
		if item.Robot == nil || item.Robot.IsPrivate {
			continue
		}
		if category != "" && category != "全部" && item.Robot.Category != category {
			continue
		}
		filtered = append(filtered, item)
	}

	total := int64(len(filtered))

	// 分页
	if offset >= len(filtered) {
		return []models.RobotRanking{}, total, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	page := filtered[offset:end]

	// 转为返回类型，附上排名和 Robot
	result := make([]models.RobotRanking, len(page))
	for i, item := range page {
		result[i] = item.RobotRanking
		result[i].Robot = item.Robot
	}

	return result, total, nil
}

// GetSingleRobotRanking 获取单个机器人在指定周期的排名和数据（开发者接口用）
func (s *Service) GetSingleRobotRanking(robotID uint, periodType, periodKey string) (rank int64, data *models.RobotRanking) {
	allItems, err := s.computeRankings(periodType, periodKey)
	if err != nil {
		return 0, nil
	}

	// 只看公开机器人的排名
	publicRank := int64(0)
	for _, item := range allItems {
		if item.Robot != nil && !item.Robot.IsPrivate {
			publicRank++
		}
		if item.RobotID == robotID {
			r := item.RobotRanking
			return publicRank, &r
		}
	}
	return 0, nil
}
