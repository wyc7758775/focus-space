package main

// This adapter keeps live device data inside the Agent-owned application. It
// uses the capability-gated loopback bridge injected by App Host and converts
// bounded meeting/Obsidian projections into the same EvidenceEnvelope used by
// SQLite and RSS. No device credential or arbitrary path crosses the browser.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"focus-space/internal/bridges"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	focusDeviceOverallTimeout = 8 * time.Second
	focusDeviceMeetingLimit   = 20
	focusDeviceDirectoryLimit = 16
	focusDeviceEntryLimit     = 200
	focusDeviceReadLimit      = 20
	focusDeviceResponseBytes  = 2 << 20
)

type focusDeviceMeeting struct {
	ID                string          `json:"id"`
	Title             string          `json:"title"`
	Status            string          `json:"status"`
	DurationSeconds   int             `json:"duration_seconds"`
	SpeakerCount      int             `json:"speaker_count"`
	AudioRef          string          `json:"audio_ref"`
	StartedAt         json.RawMessage `json:"started_at"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
	Transcript        string          `json:"transcript"`
	Summary           string          `json:"summary"`
	PerSpeakerSummary string          `json:"per_speaker_summary"`
	Report            string          `json:"report"`
}

type focusDeviceMeetingList struct {
	Meetings []focusDeviceMeeting `json:"meetings"`
}

type focusDeviceMeetingDetail struct {
	Meeting focusDeviceMeeting `json:"meeting"`
}

type focusDeviceObsidianEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	ModifiedAt string `json:"modified_at"`
}

type focusDeviceObsidianList struct {
	Entries    []focusDeviceObsidianEntry `json:"entries"`
	HasMore    bool                       `json:"has_more"`
	NextCursor string                     `json:"next_cursor"`
}

type focusDeviceObsidianRead struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func readLiveObsidianEvidence(itemKey string) (Evidence, error) {
	path, ok := liveObsidianPathFromItemKey(itemKey)
	if !ok {
		return Evidence{}, errors.New("invalid live Obsidian item key")
	}
	format, ok := liveObsidianContentFormat(path)
	if !ok {
		return Evidence{}, errors.New("unsupported live Obsidian item")
	}
	content := ""
	if format == "markdown" || format == "text" {
		bridge := bridges.NewDeviceDataBridgeFromEnv()
		if bridge == nil || bridge.Err != nil || bridge.BaseURL == nil || bridge.Client == nil {
			return Evidence{}, errors.New("device data bridge is unavailable")
		}
		ctx, cancel := context.WithTimeout(context.Background(), focusDeviceOverallTimeout)
		defer cancel()
		var read focusDeviceObsidianRead
		if err := focusDeviceGetJSON(ctx, bridge, "/obsidian/read", url.Values{"path": {path}}, &read); err != nil {
			return Evidence{}, err
		}
		content = read.Content
	}
	_, evidenceID, _ := strings.Cut(itemKey, ":")
	quote := content
	if strings.TrimSpace(quote) == "" {
		quote = map[string]string{
			"pdf":   "Obsidian 本地 PDF 文件",
			"image": "Obsidian 本地图片文件",
		}[format]
	}
	return Evidence{
		ID:            evidenceID,
		Title:         limitText(pathBase(path), 240),
		Locator:       path,
		ObservedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Quote:         limitText(quote, 1200),
		SourceType:    "obsidian",
		Content:       limitText(content, maxContextSnapshotRunes),
		ContentFormat: format,
	}, nil
}

// collectDeviceFocusCandidates is intentionally best-effort. A missing or
// temporarily unavailable bridge is represented by source status and never
// makes an otherwise usable RSS/local analysis fail.
func collectDeviceFocusCandidates(space FocusSpace, scope string, includeFullText bool, terms []string, filterSource func(string, string) bool) ([]focusCandidate, []focusSourceStatus, error) {
	bridge := bridges.NewDeviceDataBridgeFromEnv()
	if bridge == nil || bridge.Err != nil || bridge.BaseURL == nil || bridge.Client == nil {
		return nil, nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), focusDeviceOverallTimeout)
	defer cancel()

	results := make([]focusCandidate, 0, focusDeviceMeetingLimit+focusDeviceReadLimit)
	statuses := make([]focusSourceStatus, 0, 2)
	var sourceErrors []error

	meetingsStatus := focusSourceStatus{SourceKind: "meeting", Status: "ok"}
	var meetingList focusDeviceMeetingList
	if err := focusDeviceGetJSON(ctx, bridge, "/meetings", url.Values{"limit": {fmt.Sprint(focusDeviceMeetingLimit)}, "offset": {"0"}}, &meetingList); err != nil {
		meetingsStatus.Status = "unavailable"
		meetingsStatus.Message = "device_meetings_unavailable"
		sourceErrors = append(sourceErrors, err)
	} else {
		meetingsStatus.RecordsSeen = len(meetingList.Meetings)
		for _, listed := range meetingList.Meetings {
			if strings.TrimSpace(listed.ID) == "" || !filterSource("meeting", listed.ID) {
				continue
			}
			observed := focusDeviceMeetingTime(listed.StartedAt, firstNonEmpty(listed.UpdatedAt, listed.CreatedAt))
			if !focusDateWithinHorizon(space, observed, scope) {
				continue
			}
			meeting := listed
			var detail focusDeviceMeetingDetail
			if err := focusDeviceGetJSON(ctx, bridge, "/meetings/"+url.PathEscape(listed.ID), nil, &detail); err == nil && detail.Meeting.ID != "" {
				meeting = detail.Meeting
			} else if err != nil {
				// The metadata title is still useful evidence, but surface the
				// partial read instead of pretending the transcript was available.
				meetingsStatus.Status = "partial"
				meetingsStatus.Message = "meeting_detail_partial"
			}
			excerpt := firstNonEmpty(meeting.Summary, meeting.Report, meeting.Transcript, meeting.Title)
			if !includeFullText {
				excerpt = limitText(excerpt, 2400)
			} else {
				excerpt = limitText(excerpt, focusEvidenceExcerptMax)
			}
			version := shortDigest(strings.Join([]string{meeting.ID, meeting.Title, observed, excerpt, meeting.Status}, "\x00"))
			envelope := focusEvidenceEnvelope{
				ID: "meeting:" + meeting.ID, SourceKind: "meeting", SourceID: meeting.ID,
				SourceRevision: version, Title: limitText(meeting.Title, 240), Locator: "meeting:" + meeting.ID,
				Quote: excerpt, TextHash: articleTextHash(excerpt),
				SourceLocator: map[string]any{"recording_id": meeting.ID},
				ObservedAt:    observed, Excerpt: excerpt, AccessScope: "owner/profile-scoped",
			}
			score := scoreFocusText(terms, envelope.Title, envelope.Excerpt)
			if score > 0 {
				meetingsStatus.RecordsUsed++
				results = append(results, focusCandidate{Envelope: envelope, Score: score, Date: observed})
			}
		}
	}
	statuses = append(statuses, meetingsStatus)

	obsidianStatus := focusSourceStatus{SourceKind: "obsidian", Status: "ok"}
	notes, noteErr := focusDeviceListObsidian(ctx, bridge)
	if noteErr != nil {
		obsidianStatus.Status = "unavailable"
		obsidianStatus.Message = "obsidian_unavailable"
		sourceErrors = append(sourceErrors, noteErr)
	} else {
		obsidianStatus.RecordsSeen = len(notes)
		// Read the most recently changed bounded set. The path/title is
		// considered first, so an unrelated large vault never becomes a prompt.
		sort.SliceStable(notes, func(i, j int) bool {
			return notes[i].ModifiedAt > notes[j].ModifiedAt || (notes[i].ModifiedAt == notes[j].ModifiedAt && notes[i].Path < notes[j].Path)
		})
		if len(notes) > focusDeviceReadLimit {
			notes = notes[:focusDeviceReadLimit]
		}
		for _, note := range notes {
			if note.Path == "" || !filterSource("obsidian", note.Path) || !focusDateWithinHorizon(space, note.ModifiedAt, scope) {
				continue
			}
			var read focusDeviceObsidianRead
			if err := focusDeviceGetJSON(ctx, bridge, "/obsidian/read", url.Values{"path": {note.Path}}, &read); err != nil {
				obsidianStatus.Status = "partial"
				obsidianStatus.Message = "obsidian_note_partial"
				continue
			}
			excerpt := read.Content
			if !includeFullText {
				excerpt = limitText(excerpt, 2400)
			} else {
				excerpt = limitText(excerpt, focusEvidenceExcerptMax)
			}
			title := firstNonEmpty(note.Name, pathBase(note.Path))
			version := shortDigest(strings.Join([]string{note.Path, note.ModifiedAt, read.Content}, "\x00"))
			envelope := focusEvidenceEnvelope{
				ID: "obsidian:" + note.Path, SourceKind: "obsidian", SourceID: note.Path,
				SourceRevision: version, Title: limitText(title, 240), Locator: note.Path,
				Quote: excerpt, TextHash: articleTextHash(excerpt),
				SourceLocator: map[string]any{"path": note.Path},
				ObservedAt:    note.ModifiedAt, Excerpt: excerpt, AccessScope: "owner/profile-scoped",
			}
			score := scoreFocusText(terms, envelope.Title, envelope.Excerpt)
			if score > 0 {
				obsidianStatus.RecordsUsed++
				results = append(results, focusCandidate{Envelope: envelope, Score: score, Date: note.ModifiedAt})
			}
		}
	}
	statuses = append(statuses, obsidianStatus)

	if len(results) == 0 && len(sourceErrors) == len(statuses) {
		return nil, statuses, errors.Join(sourceErrors...)
	}
	return results, statuses, nil
}

func focusDeviceGetJSON(ctx context.Context, bridge *bridges.DeviceDataBridge, suffix string, query url.Values, out any) error {
	if bridge == nil || bridge.Err != nil || bridge.BaseURL == nil || bridge.Client == nil {
		return errors.New("device data bridge is unavailable")
	}
	// suffix is assembled only from the fixed paths in this file. Keep a
	// defensive check here so a future caller cannot turn the bridge into an
	// arbitrary path proxy.
	if suffix != "/meetings" && suffix != "/obsidian/list" && suffix != "/obsidian/read" && !strings.HasPrefix(suffix, "/meetings/") {
		return errors.New("device data path is unavailable")
	}
	target := *bridge.BaseURL
	target.Path = strings.TrimRight(target.Path, "/") + suffix
	if query != nil {
		target.RawQuery = query.Encode()
	} else {
		target.RawQuery = ""
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set(bridges.DeviceDataTokenHeader, bridge.Token)
	request.Header.Set("Accept", "application/json")
	response, err := bridge.Client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("device data status %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, focusDeviceResponseBytes))
	if err := decoder.Decode(out); err != nil {
		return err
	}
	return nil
}

func focusDeviceListObsidian(ctx context.Context, bridge *bridges.DeviceDataBridge) ([]focusDeviceObsidianEntry, error) {
	queue := []string{""}
	seen := make(map[string]struct{}, focusDeviceDirectoryLimit)
	entries := make([]focusDeviceObsidianEntry, 0, focusDeviceEntryLimit)
	for len(queue) > 0 && len(seen) < focusDeviceDirectoryLimit && len(entries) < focusDeviceEntryLimit {
		path := queue[0]
		queue = queue[1:]
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		var page focusDeviceObsidianList
		if err := focusDeviceGetJSON(ctx, bridge, "/obsidian/list", url.Values{"path": {path}}, &page); err != nil {
			if path == "" {
				return nil, err
			}
			continue
		}
		for _, entry := range page.Entries {
			if entry.Path == "" || strings.ContainsAny(entry.Path, "\x00\r\n") {
				continue
			}
			if entry.Type == "directory" {
				if len(seen)+len(queue) < focusDeviceDirectoryLimit {
					queue = append(queue, entry.Path)
				}
				continue
			}
			if entry.Type == "markdown" || entry.Type == "text" || strings.HasSuffix(strings.ToLower(entry.Path), ".md") || strings.HasSuffix(strings.ToLower(entry.Path), ".txt") {
				entries = append(entries, entry)
				if len(entries) >= focusDeviceEntryLimit {
					break
				}
			}
		}
	}
	return entries, nil
}

func focusDeviceMeetingTime(raw json.RawMessage, fallback string) string {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return strings.TrimSpace(fallback)
	}
	if strings.HasPrefix(value, `"`) {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(text)); err == nil {
				return parsed.UTC().Format(time.RFC3339)
			}
			return strings.TrimSpace(text)
		}
	}
	var number float64
	if json.Unmarshal(raw, &number) == nil && number > 0 {
		if number > 100000000000 {
			number /= 1000
		}
		return time.Unix(int64(number), 0).UTC().Format(time.RFC3339)
	}
	return strings.TrimSpace(fallback)
}

func pathBase(path string) string {
	path = strings.TrimRight(path, "/")
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[index+1:]
	}
	return path
}

func mergeFocusSourceStatuses(base, incoming []focusSourceStatus) []focusSourceStatus {
	result := append([]focusSourceStatus(nil), base...)
	for _, next := range incoming {
		found := -1
		for index := range result {
			if result[index].SourceKind == next.SourceKind {
				found = index
				break
			}
		}
		if found < 0 {
			result = append(result, next)
			continue
		}
		current := &result[found]
		if current.Status == "unavailable" && next.Status != "unavailable" {
			*current = next
			continue
		}
		if next.Status == "unavailable" {
			// Keep a working local projection as the source of truth when the
			// optional live bridge is down.
			continue
		}
		current.RecordsSeen += next.RecordsSeen
		current.RecordsUsed += next.RecordsUsed
		if current.Status == "ok" && next.Status != "ok" {
			current.Status = "partial"
		}
		if current.Message == "" {
			current.Message = next.Message
		} else if next.Message != "" && !strings.Contains(current.Message, next.Message) {
			current.Message += ";" + next.Message
		}
	}
	return result
}
