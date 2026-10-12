package main

import (
	"strings"
	"testing"
)

func TestMeetingV3AndFocusDrawerContracts(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	styleBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	script, styles := string(scriptBytes), string(styleBytes)

	for _, expected := range []string{
		`function presentCitationDrawer(drawer, kind = "source")`,
		`existing.dataset.citationLayerKind`,
		`presentCitationDrawer(drawer, "source")`,
		`presentCitationDrawer(drawer, "workspace")`,
		`drawer.style.zIndex = String(81 + index * 2)`,
		`function renderMeetingDetail(entry, panelOverride = null, options = {})`,
		`panel.dataset.detailFingerprint = "";`,
		`function renderMeetingEntry(entry)`,
		`entry.summary || detail.summary || item.summary || report.overview || report.headline`,
		`entry.kind === "recording"`,
		`if (renderMeetingDetail(entry)) return;`,
		`function meetingReportObject(raw)`,
		`meetingStructuredMarkdown(rawSummary)`,
		`const highlights = reportHighlights.length`,
		`const actions = reportActions.length`,
		`data-meeting-tab="summary"`,
		`data-meeting-tab="transcript"`,
		`data-meeting-seek=`,
		`data-meeting-play`,
		`data-meeting-rate`,
		`id="meeting-v3-audio"`,
		`function storeLiveMeetingPreference(id, payload)`,
		`if (audio.readyState >= 1) restorePlayback();`,
		`autoplayAfterMetadata = autoplayAfterMetadata || autoplay`,
		`Number.isFinite(audio.duration) && audio.duration > 0`,
		`seekTo(button.dataset.meetingSeek, true);`,
		`sync();`,
		`const baseRecordingMutation = recordingMutation`,
		`if (!liveMeetingEntry(id)) return baseRecordingMutation(id, payload);`,
		`entry.kind === "recording" && !entry.item?.live_device`,
		`speaker_names:`,
		`audio_ref: text(item.audio_ref)`,
		`segments: (Array.isArray(item.segments)`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("meeting/drawer UI is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`.focus-citation-drawer{position:fixed;top:0;`,
		`z-index:81;`,
		`.focus-material-copy strong{display:-webkit-box;`,
		`.focus-material-copy>span{display:-webkit-box;`,
		`-webkit-line-clamp:2`,
		`.meeting-v3-detail-panel{`,
		`.meeting-v3-row{`,
		`.meeting-v3-row-main{`,
		`.meeting-v3-tabs{`,
		`.meeting-v3-transcript{`,
		`.meeting-v3-scroll{min-height:0;flex:1;display:flex;flex-direction:column;`,
		`.meeting-v3-player{position:sticky;bottom:0;`,
		`width:100%;max-width:760px;margin:auto 0 0;`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("meeting/drawer styles are missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`bottom:-24px`,
		`margin:12px -4px -24px`,
	} {
		if strings.Contains(styles, forbidden) {
			t.Errorf("meeting player must remain fully inside the scroll viewport; found %q", forbidden)
		}
	}
}
