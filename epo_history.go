package main

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	htmlnode "golang.org/x/net/html"
)

const (
	epoHistoryIndexEndpoint = "https://data.epo.org/publication-server/rest/v1.2/publication-dates"
	epoHistoryMaxDates      = 10
	epoHistoryPerDateLimit  = 1000
	epoHistoryMaxItems      = 1000
	epoHistoryIndexLimit    = 4096
)

var (
	epoHistoryDatePathRE   = regexp.MustCompile(`^/publication-server/rest/v1\.2/publication-dates/([0-9]{8})/patents$`)
	epoHistoryPatentPathRE = regexp.MustCompile(`^/publication-server/rest/v1\.2/patents/(EP[0-9]{6,9}[A-Z0-9]{0,8})$`)
	epoHistoryPlainDateRE  = regexp.MustCompile(`(?:^|/)publication-dates/([0-9]{8})/patents(?:$|\s)`)
	epoHistoryPlainIDRE    = regexp.MustCompile(`(?:^|/)patents/(EP[0-9]{6,9}[A-Z0-9]{0,8})(?:$|\s)`)
)

func collectEPOHistoryOperational(ctx context.Context, source FeedSource, spec operationalSourceSpec, fetcher operationalFetchFunc, fetchedAt string) operationalSourceResult {
	if source.ID != "epo-publication-server" || source.URL != epoHistoryIndexEndpoint || len(spec.endpoints) != 1 || spec.endpoints[0] != epoHistoryIndexEndpoint || fetcher == nil {
		return operationalSourceResult{Status: "unavailable", Message: "EPO 固定历史来源配置无效"}
	}
	now := time.Now().UTC()
	if parsed, err := time.Parse(time.RFC3339, fetchedAt); err == nil {
		now = parsed.UTC()
	}
	indexResponse, err := fetcher(ctx, epoHistoryIndexEndpoint, "", "")
	if err != nil {
		return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(err), 500)}
	}
	if !validEPOHistoryResponseURL(indexResponse.FinalURL, epoHistoryIndexEndpoint) {
		return operationalSourceResult{Status: "unavailable", Message: "EPO 发布日期索引跳转到了非官方地址"}
	}
	dates, err := parseEPOHistoryDates(indexResponse.Body, now)
	if err != nil {
		return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(err), 500)}
	}

	cutoff := time.Time{}
	if source.BackfillDays > 0 {
		cutoff = now.Add(-time.Duration(source.BackfillDays) * 24 * time.Hour)
	}

	feed := makeOperationalFeed(source, nil, "api")
	seen := make(map[string]struct{}, epoHistoryMaxItems)
	status, message := "ok", ""
	readDates := 0
	for index, date := range dates {
		if index >= epoHistoryMaxDates || len(feed.Items) >= epoHistoryMaxItems {
			break
		}
		if index > 0 && !epoHistoryDateWithinCutoff(date, cutoff) {
			break
		}
		readDates++
		dateURL := epoHistoryDateURL(date)
		response, fetchErr := fetcher(ctx, dateURL, "", "")
		if fetchErr != nil {
			if len(feed.Items) == 0 {
				return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(fetchErr), 500)}
			}
			status, message = operationalPartialResult(feed, "EPO 历史发布日期读取失败："+errorText(fetchErr))
			break
		}
		if !validEPOHistoryResponseURL(response.FinalURL, dateURL) {
			if len(feed.Items) == 0 {
				return operationalSourceResult{Status: "unavailable", Message: "EPO 专利列表跳转到了非官方地址"}
			}
			status, message = operationalPartialResult(feed, "EPO 历史发布日期返回了非官方地址")
			break
		}
		patents, parseErr := parseEPOHistoryPatents(response.Body, epoHistoryMaxItems-len(feed.Items))
		if parseErr != nil {
			if len(feed.Items) == 0 {
				return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(parseErr), 500)}
			}
			status, message = operationalPartialResult(feed, "EPO 历史发布日期解析失败："+errorText(parseErr))
			break
		}
		published := epoHistoryPublishedAt(date)
		pageItems := make([]NewsItem, 0, len(patents))
		for _, patentID := range patents {
			link := "https://data.epo.org/publication-server/rest/v1.2/patents/" + patentID + "/document.html"
			pageItems = append(pageItems, makeOperationalNews(source, patentID, "EPO 公开专利 "+patentID, link, published, "欧洲专利局公开专利发布数据", "", "api", "official", ""))
		}
		appendOperationalFeedPage(&feed, makeOperationalFeed(source, pageItems, "api"), seen, epoHistoryMaxItems)
	}
	if len(feed.Items) == 0 {
		return operationalSourceResult{Status: "unavailable", Message: "EPO 没有返回可用公开专利"}
	}
	if status == "ok" && len(feed.Items) < epoHistoryMaxItems && readDates == epoHistoryMaxDates && len(dates) > readDates && epoHistoryDateWithinCutoff(dates[readDates], cutoff) {
		status = "partial"
		message = "EPO 最近发布日期返回的记录不足 1000 条，已保留当前有界窗口"
	}
	result := operationalFeedResultWithLimit(feed, status, message, feedFetchResponse{}, epoHistoryMaxItems)
	if strings.TrimSpace(source.BackfillCursor) != "" {
		result.BackfillCursor = ""
		result.BackfillCursorChanged = true
	}
	return result
}

func parseEPOHistoryDates(body []byte, now time.Time) ([]string, error) {
	dates := make(map[string]struct{})
	for _, path := range epoHistoryAnchorPaths(body, epoHistoryDatePathRE, epoHistoryIndexLimit) {
		match := epoHistoryDatePathRE.FindStringSubmatch(path)
		if len(match) == 2 && validEPOHistoryDate(match[1], now) {
			dates[match[1]] = struct{}{}
		}
	}
	// Older tests and mirrors may expose the same fixed official paths as plain
	// text. They remain safe because every follow-up URL is rebuilt locally.
	if len(dates) == 0 && !bytes.Contains(body, []byte("<")) {
		for _, match := range epoHistoryPlainDateRE.FindAllSubmatch(body, epoHistoryIndexLimit) {
			if len(match) == 2 && validEPOHistoryDate(string(match[1]), now) {
				dates[string(match[1])] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(dates))
	for date := range dates {
		result = append(result, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(result)))
	if len(result) == 0 {
		return nil, errors.New("EPO 没有返回可用官方发布日期")
	}
	return result, nil
}

func parseEPOHistoryPatents(body []byte, limit int) ([]string, error) {
	if limit < 1 || limit > epoHistoryPerDateLimit {
		return nil, errors.New("EPO 专利窗口参数无效")
	}
	patents := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	appendID := func(id string) bool {
		if _, duplicate := seen[id]; duplicate {
			return true
		}
		seen[id] = struct{}{}
		if len(patents) < limit {
			patents = append(patents, id)
			return true
		}
		return false
	}

	tokenizer := htmlnode.NewTokenizer(bytes.NewReader(body))
	for len(patents) < limit {
		switch tokenizer.Next() {
		case htmlnode.ErrorToken:
			goto parsedHTML
		case htmlnode.StartTagToken, htmlnode.SelfClosingTagToken:
			token := tokenizer.Token()
			if !strings.EqualFold(token.Data, "a") {
				continue
			}
			for _, attr := range token.Attr {
				if !strings.EqualFold(attr.Key, "href") {
					continue
				}
				path, ok := epoHistoryOfficialPath(attr.Val)
				if !ok {
					break
				}
				match := epoHistoryPatentPathRE.FindStringSubmatch(path)
				if len(match) == 2 && !appendID(match[1]) {
					goto parsedHTML
				}
				break
			}
		}
	}

parsedHTML:
	if len(seen) == 0 && !bytes.Contains(body, []byte("<")) {
		for _, match := range epoHistoryPlainIDRE.FindAllSubmatch(body, limit) {
			if len(match) == 2 && !appendID(string(match[1])) {
				break
			}
		}
	}
	if len(seen) == 0 {
		return nil, errors.New("EPO 没有返回可用官方专利编号")
	}
	return patents, nil
}

func epoHistoryAnchorPaths(body []byte, pattern *regexp.Regexp, limit int) []string {
	result := make([]string, 0, minInt(limit, epoHistoryPerDateLimit))
	tokenizer := htmlnode.NewTokenizer(bytes.NewReader(body))
	for len(result) < limit {
		switch tokenizer.Next() {
		case htmlnode.ErrorToken:
			return result
		case htmlnode.StartTagToken, htmlnode.SelfClosingTagToken:
			token := tokenizer.Token()
			if !strings.EqualFold(token.Data, "a") {
				continue
			}
			for _, attr := range token.Attr {
				if !strings.EqualFold(attr.Key, "href") {
					continue
				}
				if path, ok := epoHistoryOfficialPath(attr.Val); ok && pattern.MatchString(path) {
					result = append(result, path)
				}
				break
			}
		}
	}
	return result
}

func epoHistoryOfficialPath(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", false
	}
	if parsed.IsAbs() {
		if parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "data.epo.org") {
			return "", false
		}
	} else if parsed.Host != "" || parsed.Scheme != "" {
		return "", false
	}
	return parsed.Path, parsed.Path != ""
}

func validEPOHistoryResponseURL(raw, expected string) bool {
	if strings.TrimSpace(raw) == "" {
		raw = expected
	}
	actual, actualErr := url.Parse(raw)
	want, wantErr := url.Parse(expected)
	if actualErr != nil || wantErr != nil || actual.User != nil || actual.Port() != "" || actual.RawQuery != "" || actual.ForceQuery || actual.Fragment != "" || actual.RawPath != "" {
		return false
	}
	return actual.Scheme == "https" && strings.EqualFold(actual.Hostname(), "data.epo.org") && actual.Path == want.Path && want.Scheme == "https" && strings.EqualFold(want.Hostname(), "data.epo.org") && want.RawQuery == ""
}

func epoHistoryDateURL(date string) string {
	if _, err := time.Parse("20060102", date); err != nil {
		return ""
	}
	return epoHistoryIndexEndpoint + "/" + date + "/patents"
}

func epoHistoryDateWithinCutoff(date string, cutoff time.Time) bool {
	if cutoff.IsZero() {
		return true
	}
	parsed, err := time.Parse("20060102", date)
	return err == nil && !parsed.Before(cutoff)
}

func validEPOHistoryDate(date string, now time.Time) bool {
	parsed, err := time.Parse("20060102", date)
	return err == nil && !parsed.After(now.UTC().Add(24*time.Hour))
}

func epoHistoryPublishedAt(date string) string {
	parsed, err := time.Parse("20060102", date)
	if err != nil {
		return ""
	}
	return parsed.UTC().Format(time.RFC3339)
}
