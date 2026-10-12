package main

import (
	"strings"
	"testing"
)

func TestScheduleBridgeCandidateReleaseAndCapabilityRetryUnit(t *testing.T) {
	body, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	section := func(start, end string) string {
		t.Helper()
		from := strings.Index(page, start)
		if from < 0 {
			t.Fatalf("missing section start %q", start)
		}
		to := strings.Index(page[from:], end)
		if to < 0 {
			t.Fatalf("missing section end %q", end)
		}
		return page[from : from+to]
	}

	candidate := section("function scheduleCandidateForAction", "function actionFieldsFromPanel")
	if !strings.Contains(candidate, "candidate_id: action.id") || strings.Contains(candidate, "action.schedule_candidate_id") {
		t.Fatalf("candidate id must be the exact action id: %s", candidate)
	}
	begin := section("async function beginActionSchedule", "function installActionWorkflowControl")
	if !strings.Contains(begin, "candidate_id: action.id") || !strings.Contains(begin, "server_pending: existing?.server_pending === true") {
		t.Fatalf("candidate send must preserve identity without claiming server pending: %s", begin)
	}
	restore := section("function restorePendingSchedules", "function actionByCandidate")
	for _, expected := range []string{
		`text(action.schedule_state) !== "pending"`,
		"const candidateID = text(action.id)",
		"server_pending: true",
		"pendingStorageWrite(restored)",
	} {
		if !strings.Contains(restore, expected) {
			t.Fatalf("server pending restore missing %q", expected)
		}
	}
	if strings.Contains(restore, `action.schedule_state === "pending" || local.request_id`) || strings.Contains(restore, "action.schedule_candidate_id") {
		t.Fatalf("local candidate must not be restored as server pending: %s", restore)
	}

	release := section("function validScheduleRelease", "async function receiveScheduleReceipt")
	for _, expected := range []string{
		`schedule_candidate_release_v1`,
		`new Set(["type", "request_id", "candidate_id", "revision"])`,
		"pending.server_pending",
		"pending.request_id !== value.request_id",
		"pending.candidate_id !== value.candidate_id",
		"Number(pending.revision) !== value.revision",
	} {
		if !strings.Contains(page, expected) && !strings.Contains(release, expected) {
			t.Fatalf("release contract missing %q", expected)
		}
	}
	if !strings.Contains(page, `value.type === bridgeReleaseType && bridgeEventTrusted(event)`) {
		t.Fatal("release messages must require the trusted host")
	}
	workflow := section("function installActionWorkflowControl", "function safeEntryHTML")
	if !strings.Contains(workflow, "再次打开确认") || strings.Contains(workflow, `scheduleState === "pending" || candidatePending ? "disabled"`) {
		t.Fatal("a display-only candidate must remain retryable after host cancellation")
	}

	probe := section("function stopHostCapabilityProbe", "function pendingStorageRead")
	for _, expected := range []string{
		"bridgeCapabilityRetryDelays.length",
		"window.clearTimeout(parity.capabilityProbeTimer)",
		"!parity.host.native && window.parent === window",
		"document.visibilityState === \"hidden\"",
	} {
		if !strings.Contains(probe, expected) {
			t.Fatalf("bounded capability probe missing %q", expected)
		}
	}
	if !staticContains(page, `window.addEventListener("pagehide", stopHostCapabilityProbe)`) || !staticContains(page, `document.addEventListener("visibilitychange", onScheduleBridgeVisibilityChange)`) {
		t.Fatal("capability retry timers must stop when the page is not active")
	}
}
