package main

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// readerFocusMaintenanceState deliberately treats an old database (created
// before migration 048) as "no focus".  This keeps the existing maintenance
// command usable during an application upgrade and avoids making a sidecar
// feature a hard dependency of the Reader path.
func readerFocusMaintenanceState() (ReaderFocusState, bool, error) {
	state, err := readReaderFocusState()
	if err != nil {
		if readerFocusSchemaMissing(err) {
			return ReaderFocusState{}, false, nil
		}
		return ReaderFocusState{}, false, err
	}
	active := len(readerFocusMaintenanceTopics(state)) > 0
	return state, active, nil
}

func readerFocusSchemaMissing(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table") || strings.Contains(message, "no such column")
}

func enabledReaderFocusTopics(topics []ReaderFocusTopic) []ReaderFocusTopic {
	result := make([]ReaderFocusTopic, 0, len(topics))
	for _, topic := range topics {
		if topic.Enabled && len(topic.Terms) > 0 {
			result = append(result, topic)
		}
	}
	return result
}

// readerFocusMaintenanceTopics is narrower than the recommendation set. A
// topic must have an explicit bootstrap decision before unattended analysis
// may process it; newly added or paused topics remain recommendation-only until
// the user selects a scope for that topic.
func readerFocusMaintenanceTopics(state ReaderFocusState) []ReaderFocusTopic {
	result := make([]ReaderFocusTopic, 0, len(state.Topics))
	for _, topic := range state.Topics {
		if !topic.Enabled || len(topic.Terms) == 0 {
			continue
		}
		// 049 stores the decision on each topic. Keep the policy-based branch for
		// databases created by the initial 048 carrier before the additive
		// migration was applied.
		if topic.BootstrapMode != "" && topic.BootstrapMode != "none" {
			if topic.BootstrapMode == "analyze" && topic.BootstrapStatus == readerFocusBootstrapActive {
				result = append(result, topic)
			} else if topic.BootstrapMode == "future_only" && (topic.BootstrapStatus == readerFocusBootstrapActive || topic.BootstrapStatus == readerFocusBootstrapDone) {
				result = append(result, topic)
			}
			continue
		}
		switch state.Policy.Mode {
		case readerFocusModeAutoBootstrap:
			if topic.BootstrapStatus != readerFocusBootstrapActive {
				continue
			}
		case readerFocusModeAutoNew:
			if topic.BootstrapStatus != readerFocusBootstrapActive && topic.BootstrapStatus != readerFocusBootstrapDone {
				continue
			}
		default:
			continue
		}
		result = append(result, topic)
	}
	return result
}

// readerFocusCandidateScope derives accounting from the topic that matched
// this article. The aggregate policy mode is retained only for pre-049
// databases; it must not make an explicit future-only topic consume bootstrap
// quota when another topic is bootstrapping.
func readerFocusCandidateScope(candidate readerFocusCandidate, state ReaderFocusState) (string, []string) {
	bootstrapIDs := make([]string, 0, len(state.Topics))
	dailyIDs := make([]string, 0, len(state.Topics))
	for _, topic := range readerFocusMaintenanceTopics(state) {
		score, _, _ := scoreReaderFocusCandidate(candidate, []ReaderFocusTopic{topic})
		if score <= 0 {
			continue
		}
		mode := topic.BootstrapMode
		if mode == "" || mode == "none" {
			switch state.Policy.Mode {
			case readerFocusModeAutoBootstrap:
				mode = "analyze"
			case readerFocusModeAutoNew:
				mode = "future_only"
			}
		}
		switch mode {
		case "analyze":
			candidateForTopic := candidate
			candidateForTopic.Topics = []string{topic.Label}
			if topic.BootstrapStatus == readerFocusBootstrapActive && !readerFocusCandidateIsNew(candidateForTopic, []ReaderFocusTopic{topic}) {
				bootstrapIDs = append(bootstrapIDs, topic.ID)
			}
		case "future_only":
			candidateForTopic := candidate
			candidateForTopic.Topics = []string{topic.Label}
			if (topic.BootstrapStatus == readerFocusBootstrapActive || topic.BootstrapStatus == readerFocusBootstrapDone) && readerFocusCandidateIsNew(candidateForTopic, []ReaderFocusTopic{topic}) {
				dailyIDs = append(dailyIDs, topic.ID)
			}
		}
	}
	if len(bootstrapIDs) > 0 {
		sort.Strings(bootstrapIDs)
		return "bootstrap", bootstrapIDs
	}
	if len(dailyIDs) > 0 {
		sort.Strings(dailyIDs)
		return "daily", dailyIDs
	}
	return "", nil
}

func readerFocusCandidateIsMaintenanceEligible(candidate readerFocusCandidate, state ReaderFocusState) bool {
	if !candidate.AutoExtract {
		return false
	}
	scope, _ := readerFocusCandidateScope(candidate, state)
	return scope != ""
}

func readerFocusToday() string { return time.Now().UTC().Format("2006-01-02") }

func readerFocusUsedArticles(day string) (int, error) {
	var used int
	err := db.QueryRow(`SELECT COALESCE(SUM(CASE
	WHEN status IN ('pending','failed') AND candidate_count>processed_count THEN candidate_count
	ELSE processed_count END),0)
FROM reader_focus_runs WHERE run_day=? AND status IN ('pending','completed','partial','failed')`, day).Scan(&used)
	return used, err
}

func readerFocusMaintenanceLimit(state ReaderFocusState) (int, error) {
	perRun := state.Policy.PerRunLimit
	if perRun < 1 || perRun > readerFocusPerRunLimit {
		perRun = readerFocusPerRunLimit
	}
	used, err := readerFocusUsedArticles(readerFocusToday())
	if err != nil {
		return 0, err
	}
	remaining := state.Policy.DailyArticleLimit - used
	if remaining < 0 {
		remaining = 0
	}
	if perRun > remaining {
		perRun = remaining
	}
	return perRun, nil
}

// readerFocusBootstrapTopicsHaveCapacity keeps a candidate's topic assignment
// deterministic between the read reservation and apply validation. An article
// matching multiple bootstrap topics must fit every matching topic; otherwise
// apply could charge a topic that the read side silently omitted.
func readerFocusBootstrapTopicsHaveCapacity(candidate readerFocusCandidate, state ReaderFocusState, pending map[string]int) bool {
	scope, topicIDs := readerFocusCandidateScope(candidate, state)
	if scope != "bootstrap" || len(topicIDs) == 0 {
		return false
	}
	byID := make(map[string]ReaderFocusTopic, len(state.Topics))
	for _, topic := range state.Topics {
		byID[topic.ID] = topic
	}
	for _, topicID := range topicIDs {
		topic, ok := byID[topicID]
		if !ok {
			return false
		}
		used := topic.BootstrapProcessedCount
		if used < 0 {
			used = 0
		}
		used += pending[topicID]
		if used >= readerFocusTopicBootstrapLimit(topic, state.Policy) {
			return false
		}
	}
	return true
}

func readFocusedOverviewCandidates(state ReaderFocusState, limit int) ([]readerMaintenanceOverviewCandidate, error) {
	if limit <= 0 {
		return []readerMaintenanceOverviewCandidate{}, nil
	}
	for index := range state.Topics {
		topic := &state.Topics[index]
		if topic.BootstrapMode != "analyze" || topic.BootstrapStatus != readerFocusBootstrapActive {
			continue
		}
		used, err := readerFocusTopicBootstrapUsed(topic.ID)
		if err != nil {
			return nil, err
		}
		topic.BootstrapProcessedCount = used
	}
	candidates, err := queryReaderFocusCandidates(readerFocusMaintenanceTopics(state))
	if err != nil {
		return nil, err
	}
	receipts, err := readerFocusReceiptHashes()
	if err != nil {
		return nil, err
	}
	claimedKeys, err := readerFocusClaimExcludedKeys(readerFocusToday())
	if err != nil {
		return nil, err
	}
	usedTokens, err := readerFocusUsedTokens(readerFocusToday())
	if err != nil {
		return nil, err
	}
	remainingTokens := state.Policy.DailyTokenBudget - usedTokens
	if remainingTokens < 0 {
		remainingTokens = 0
	}
	items := make([]readerMaintenanceOverviewCandidate, 0, limit)
	reservedHigh := 0
	sort.SliceStable(candidates, func(i, j int) bool {
		leftReady := candidates[i].Overview != nil && candidates[i].Overview.Status == "ready"
		rightReady := candidates[j].Overview != nil && candidates[j].Overview.Status == "ready"
		if leftReady != rightReady {
			return leftReady
		}
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].PublishedAt != candidates[j].PublishedAt {
			return candidates[i].PublishedAt > candidates[j].PublishedAt
		}
		return candidates[i].ID < candidates[j].ID
	})
	// Keep one durable run in one accounting scope. Bootstrap candidates take
	// priority; daily future-only candidates remain queued for the next round.
	batchScope := ""
	bootstrapPending, err := readerFocusPendingTopicCounts(readerFocusToday())
	if err != nil {
		return nil, err
	}
	if bootstrapPending == nil {
		bootstrapPending = make(map[string]int, len(state.Topics))
	}
	for _, candidate := range candidates {
		if !readerFocusCandidateIsMaintenanceEligible(candidate, state) || candidate.Score <= 0 || candidate.Hash == "" || receipts[candidate.Hash] || candidate.Overview != nil || hasStringKey(claimedKeys, candidate.ID+"\x00"+candidate.Hash) {
			continue
		}
		scope, _ := readerFocusCandidateScope(candidate, state)
		if scope == "bootstrap" && readerFocusBootstrapTopicsHaveCapacity(candidate, state, bootstrapPending) {
			batchScope = scope
			break
		}
		if scope == "daily" && batchScope == "" {
			batchScope = scope
		}
	}
	for _, candidate := range candidates {
		if !readerFocusCandidateIsMaintenanceEligible(candidate, state) || candidate.Score <= 0 || candidate.Hash == "" || receipts[candidate.Hash] || candidate.Overview != nil || hasStringKey(claimedKeys, candidate.ID+"\x00"+candidate.Hash) {
			continue
		}
		candidateScope, topicIDs := readerFocusCandidateScope(candidate, state)
		if candidateScope == "" || candidateScope != batchScope {
			continue
		}
		if batchScope == "bootstrap" && !readerFocusBootstrapTopicsHaveCapacity(candidate, state, bootstrapPending) {
			continue
		}
		analysisText, err := readerFocusAnalysisText(candidate, state.Policy)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(analysisText) == "" {
			continue
		}
		_, high := estimateReaderFocusTokens(analysisText)
		if reservedHigh+high > remainingTokens {
			continue
		}
		items = append(items, readerMaintenanceOverviewCandidate{
			EntryID: candidate.ID, ContentHash: candidate.Hash, Title: candidate.Title,
			Source: candidate.Source, PublishedAt: candidate.PublishedAt,
			Excerpt: limitText(candidate.Summary, maintenanceOverviewExcerptRunes),
		})
		if batchScope == "bootstrap" {
			for _, topicID := range topicIDs {
				bootstrapPending[topicID]++
			}
		}
		reservedHigh += high
		if len(items) == limit {
			break
		}
	}
	return items, nil
}

func readFocusedMaintenanceEntries(state ReaderFocusState, limit int) ([]readerMaintenanceEntry, error) {
	if limit <= 0 {
		return []readerMaintenanceEntry{}, nil
	}
	candidates, err := queryReaderFocusCandidates(readerFocusMaintenanceTopics(state))
	if err != nil {
		return nil, err
	}
	receipts, err := readerFocusReceiptHashes()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].PublishedAt != candidates[j].PublishedAt {
			return candidates[i].PublishedAt > candidates[j].PublishedAt
		}
		return candidates[i].ID < candidates[j].ID
	})
	items := make([]readerMaintenanceEntry, 0, limit)
	perSource := make(map[string]int, limit)
	for _, candidate := range candidates {
		if candidate.Score <= 0 || candidate.Hash == "" || receipts[candidate.Hash] || candidate.Overview != nil {
			continue
		}
		if !readerFocusCandidateIsMaintenanceEligible(candidate, state) {
			continue
		}
		if perSource[candidate.SourceID] >= maintenanceReadEntryLimit {
			continue
		}
		items = append(items, readerMaintenanceEntry{
			ID: candidate.ID, SourceID: candidate.SourceID, ContentHash: candidate.Hash,
			Title: candidate.Title, SourceURL: candidate.URL, Source: candidate.Source,
			PublishedAt: candidate.PublishedAt, SummaryText: limitText(candidate.Summary, maintenanceOverviewExcerptRunes),
		})
		perSource[candidate.SourceID]++
		if len(items) == limit {
			break
		}
	}
	return items, nil
}

func readerFocusReceiptHashes() (map[string]bool, error) {
	rows, err := db.Query(`SELECT content_hash FROM reader_digest_receipts WHERE content_hash<>'' ORDER BY processed_at DESC LIMIT ?`, maxReaderDigestReceipts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]bool)
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		result[hash] = true
	}
	return result, rows.Err()
}

// validateReaderFocusMaintenanceApply is called after the normal bounded
// maintenance validation.  It re-derives the candidate set from SQLite and
// never trusts model-provided topic IDs, article counts, or token estimates.
func validateReaderFocusMaintenanceApply(d *Dashboard) error {
	state, active, err := readerFocusMaintenanceState()
	if err != nil {
		return err
	}
	if key, _ := readerFocusRunKey(state, *d); key != "" {
		if exists, existsErr := readerFocusRunExists(key); existsErr != nil {
			return existsErr
		} else if exists {
			// Idempotent replay must be accepted even after the topic has
			// transitioned from bootstrap to its steady state.
			return nil
		}
	}
	claimArticles, claimTokens, claimMatched, err := readerFocusClaimReservationBudget(*d)
	if err != nil {
		return err
	}
	if !active {
		if claimMatched {
			// A topic changed after read. Do not silently downgrade an already
			// reserved focus batch into the generic overview path: either its
			// original scope is still active or the whole apply fails atomically.
			return errors.New("关注主题状态已变化，请重新确认分析范围")
		}
		if len(d.ArticleAIOverviews) > maintenanceOverviewLimit {
			return errors.New("无关注主题时每轮最多生成 3 篇概览")
		}
		return nil
	}
	limit, err := readerFocusMaintenanceLimit(state)
	if err != nil {
		return err
	}
	if claimMatched {
		// readerFocusMaintenanceLimit includes pending reservations. Add this
		// exact batch back before validating its apply, while retaining per-run
		// and daily hard caps.
		limit += claimArticles
		if limit > state.Policy.PerRunLimit {
			limit = state.Policy.PerRunLimit
		}
		if limit > readerFocusPerRunLimit {
			limit = readerFocusPerRunLimit
		}
	}
	if len(d.ArticleAIOverviews) > limit {
		return fmt.Errorf("关注主题本轮最多生成 %d 篇概览", limit)
	}
	if len(d.ArticleAIOverviews) == 0 {
		return nil
	}
	usedTokens, err := readerFocusUsedTokens(readerFocusToday())
	if err != nil {
		return err
	}
	if claimMatched {
		// The read already reserved this batch. Validate the payload against the
		// reservation, but do not charge its estimate a second time.
		usedTokens -= claimTokens
		if usedTokens < 0 {
			usedTokens = 0
		}
	}
	// Replays may arrive after bootstrap completion switched the policy to
	// auto_new. Build the lookup from all enabled topics so a previously ready
	// historical overview remains replayable; fresh writes are still restricted
	// to the explicitly activated maintenance topics below.
	candidates, err := queryReaderFocusCandidates(enabledReaderFocusTopics(state.Topics))
	if err != nil {
		return err
	}
	byEntry := make(map[string]readerFocusCandidate, len(candidates))
	for _, candidate := range candidates {
		if candidate.Score <= 0 {
			continue
		}
		byEntry[candidate.ID] = candidate
	}
	totalHigh := 0
	runScope := ""
	bootstrapTopicCounts := make(map[string]int)
	for _, overview := range d.ArticleAIOverviews {
		candidate, ok := byEntry[overview.EntryID]
		if !ok || candidate.Hash != overview.ContentHash {
			return errors.New("概览必须引用当前未读且命中关注主题的文章")
		}
		if candidate.Overview != nil {
			if candidate.Overview.Status == "ready" && candidate.Overview.ContentHash == overview.ContentHash {
				// Idempotent replays may arrive after the bootstrap-to-daily
				// transition and carry a historical ready overview. It consumes
				// no additional budget and is safe to pass through.
				continue
			}
			return errors.New("文章概览已处理，失败内容需要用户显式重试")
		}
		if !candidate.AutoExtract {
			return errors.New("文章来源未开启自动分析")
		}
		candidateScope, topicIDs := readerFocusCandidateScope(candidate, state)
		if candidateScope == "" {
			return errors.New("概览必须引用当前维护范围内的新文章")
		}
		if runScope == "" {
			runScope = candidateScope
		} else if runScope != candidateScope {
			return errors.New("同一轮概览不能混合首次分析和后续新文章")
		}
		if candidateScope == "bootstrap" {
			for _, topicID := range topicIDs {
				bootstrapTopicCounts[topicID]++
			}
		}
		analysisText, textErr := readerFocusAnalysisText(candidate, state.Policy)
		if textErr != nil {
			return textErr
		}
		_, high := estimateReaderFocusTokens(analysisText)
		totalHigh += high
	}
	if runScope == "bootstrap" {
		for topicID, count := range bootstrapTopicCounts {
			usedBootstrap, bootstrapErr := readerFocusTopicBootstrapUsed(topicID)
			if bootstrapErr != nil {
				return bootstrapErr
			}
			var topic ReaderFocusTopic
			found := false
			for _, candidateTopic := range state.Topics {
				if candidateTopic.ID == topicID {
					topic = candidateTopic
					found = true
					break
				}
			}
			if !found || (!claimMatched && usedBootstrap+count > readerFocusTopicBootstrapLimit(topic, state.Policy)) {
				return errors.New("关注主题首次分析额度已达到上限")
			}
		}
	}
	if usedTokens+totalHigh > state.Policy.DailyTokenBudget {
		return errors.New("关注主题今日预估 token 预算不足")
	}
	return nil
}

func readerFocusRunExists(key string) (bool, error) {
	var existing int
	err := db.QueryRow(`SELECT 1 FROM reader_focus_runs WHERE idempotency_key=? LIMIT 1`, key).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
