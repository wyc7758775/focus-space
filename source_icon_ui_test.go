package main

import (
	"strings"
	"testing"
)

func TestSourceIconBackgroundLoaderIsBoundedUnit(t *testing.T) {
	raw, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	compact := strings.Join(strings.Fields(script), "")

	for _, expected := range []string{
		`constsourceIconLoadLimit=2;`,
		`constsourceIconBatchLimit=8;`,
		`constsourceIconQueueLimit=8;`,
		`constqueuedSourceIcons=newWeakSet();`,
		`sourceIconQueue.push({image,url});`,
		`while(sourceIconActive<sourceIconLoadLimit&&sourceIconQueue.length)`,
		`window.requestIdleCallback(scan,{timeout:1000});`,
		`newIntersectionObserver(`,
		`{rootMargin:"80px0px",threshold:0.01}`,
		`document.addEventListener("scroll",scheduleSourceIconBudgetRefill,`,
		`data-source-icon-src="${esc(deferredIcon)}"`,
		`fetchpriority="low"`,
		`alt=""`,
		`aria-hidden="true"`,
	} {
		if !strings.Contains(compact, expected) {
			t.Errorf("background source icon loader is missing %q", expected)
		}
	}

	avatarStart := strings.Index(script, `function sourceAvatar(`)
	if avatarStart < 0 {
		t.Fatal("source avatar renderer is missing")
	}
	avatarEnd := strings.Index(script[avatarStart:], `function sourceGroupStorageKey(`)
	if avatarEnd < 0 {
		t.Fatal("source avatar renderer boundaries are missing")
	}
	avatar := script[avatarStart : avatarStart+avatarEnd]
	if strings.Contains(avatar, `src="${esc(remoteIcon`) || strings.Contains(avatar, `src="${esc(backgroundIcon`) {
		t.Fatal("source avatar must not start a remote icon request during first render")
	}
	if !strings.Contains(avatar, `scheduleSourceIconScan(true);`) {
		t.Fatal("source avatar did not schedule its deferred icon scan")
	}
	if !strings.Contains(avatar, `const priorityIcon = isVideoCreator || isXCreator;`) ||
		!strings.Contains(avatar, `const immediateIcon = priorityIcon ? remoteIcon : "";`) {
		t.Fatal("video and X creator portraits are not prioritized through the app-local endpoint")
	}
}

func TestSourceIconBackgroundLoaderKeepsRetryAndFallbackUnit(t *testing.T) {
	raw, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	compact := strings.Join(strings.Fields(string(raw)), "")
	for _, expected := range []string{
		`functionretrySourceIcon(image){`,
		"retry.split(\"#\",1)[0]+`&retry=${Date.now()}`",
		`image.dataset.brandFallbackAttempted="1";`,
		`queueSourceIconRequest(image,brandFallback,true);`,
		`constfallback=text(image.dataset.fallbackSrc);`,
		`image.src=fallback;`,
		`finishSourceIconRequest(event.target);`,
	} {
		if !strings.Contains(compact, expected) {
			t.Errorf("source icon recovery chain is missing %q", expected)
		}
	}
}

func TestSourceIconBackgroundLoaderDefersVisibleRequestFlow(t *testing.T) {
	raw, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)

	observeStart := strings.Index(script, "function observeSourceIcons(")
	if observeStart < 0 {
		t.Fatal("source icon observer is missing")
	}
	observeEnd := strings.Index(script[observeStart:], "function scheduleSourceIconScan(")
	if observeEnd < 0 {
		t.Fatal("source icon observer boundaries are missing")
	}
	observer := script[observeStart : observeStart+observeEnd]
	for _, expected := range []string{
		"new IntersectionObserver(",
		"entry.isIntersecting",
		"queueSourceIconRequest(",
		`image.dataset.sourceIconPriority === "1"`,
		"observer.unobserve(image)",
	} {
		if !strings.Contains(observer, expected) {
			t.Errorf("visible source icon flow is missing %q", expected)
		}
	}

	scanStart := strings.Index(script, "function scheduleSourceIconScan(")
	if scanStart < 0 {
		t.Fatal("source icon scan scheduler is missing")
	}
	scanEnd := strings.Index(script[scanStart:], "function retrySourceIcon(")
	if scanEnd < 0 {
		t.Fatal("source icon scan scheduler boundaries are missing")
	}
	scheduler := script[scanStart : scanStart+scanEnd]
	if !strings.Contains(scheduler, "requestIdleCallback(scan, { timeout: 1000 })") {
		t.Fatal("source icon scan is not scheduled during idle time")
	}
	if strings.Contains(scheduler, "image.src") {
		t.Fatal("source icon scheduler must not issue a request before visibility observation")
	}

	loadListener := strings.Index(script, "document.addEventListener(\n    \"load\",")
	if loadListener < 0 {
		t.Fatal("source icon load listener is missing")
	}
	loadHandler := strings.Index(script[loadListener:], `event.target.matches(".source-avatar-image")`)
	if loadHandler < 0 {
		t.Fatal("source icon load handler is missing")
	}
	loadHandler += loadListener
	loadEnd := strings.Index(script[loadHandler:], "installScheduleBridge()")
	if loadEnd < 0 {
		t.Fatal("source icon load handler boundaries are missing")
	}
	if !strings.Contains(script[loadHandler:loadHandler+loadEnd], "finishSourceIconRequest(event.target)") {
		t.Fatal("source icon load flow does not release its concurrency slot")
	}
}
