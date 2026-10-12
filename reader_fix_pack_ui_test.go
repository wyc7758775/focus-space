package main

import (
	"strings"
	"testing"
)

// TestVideoCoverPlaceholderTerminalStates 视频卡封面占位必须区分两种终态：
// 网关没回传缩略图（B 站条目常见）是"暂无封面"，两级回退都失败是"封面加载
// 失败"。两者都不许再挂着"加载中"——列表里会出现永远转不完的假 loading。
func TestVideoCoverPlaceholderTerminalStates(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		`const placeholderLabel = video.thumbnailURL`,
		`readerFocusCopy("暂无封面", "No cover")`,
		`class="youtube-thumbnail-placeholder-label"`,
		`readerFocusCopy("封面加载失败", "Cover unavailable")`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("video cover placeholder terminal state is missing %q", expected)
		}
	}
}

// TestDeviceCountsColdStartRetry 冷启动紧跟代理唤醒窗口，首个 counts 请求可能
// 被唤醒 503 吃掉且之后没有任何事件再触发。启动路径必须带一份有界退避重试；
// 进入设备视图的用户行为仍然走原调度，不叠加第二套机制。
func TestDeviceCountsColdStartRetry(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		`async function loadDeviceCounts(retryBudget = 0)`,
		`void loadDeviceCounts(retryBudget - 1);`,
		`void loadDeviceCounts(2);`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("device counts cold-start retry is missing %q", expected)
		}
	}
	// 非启动调用（手动刷新桥）不得携带重试预算，重试只属于冷启动窗口。
	if strings.Contains(script, `__plaudRefreshReaderCounts = () => Promise.allSettled([loadEntryCounts(true), loadDeviceCounts()])`) == false {
		t.Error("manual refresh bridge must keep loadDeviceCounts() without a retry budget")
	}
}

// TestAggregateViewColdStartAutoRetry 聚合视图首个分页请求撞上唤醒 503 时，
// 前台视图静默重试一次（有界），而不是把"必须手点重试"当作冷启动路径。
func TestAggregateViewColdStartAutoRetry(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		`entryViewAutoRetries: new Map(),`,
		`parity.entryViewAutoRetries.get(target) || 0`,
		`void loadAggregateEntriesOnDemand(target);`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("aggregate view cold-start auto retry is missing %q", expected)
		}
	}
}
