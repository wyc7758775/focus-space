package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"focus-space/internal/bridges"
	"net/url"
	"strings"
)

// The Reader UI gets meetings from the capability-gated device bridge. App
// operations must expose the same owner-scoped records so Chat can resolve a
// meeting source persisted on a Focus card instead of seeing only its opaque ID.
func queryDeviceRecordings(parent context.Context, limit int) ([]Recording, error) {
	if limit < 1 || limit > focusDeviceMeetingLimit {
		limit = focusDeviceMeetingLimit
	}
	bridge := bridges.NewDeviceDataBridgeFromEnv()
	if bridge == nil || bridge.Err != nil {
		return nil, errors.New("device meeting bridge is unavailable")
	}
	ctx, cancel := context.WithTimeout(parent, focusDeviceOverallTimeout)
	defer cancel()
	var response focusDeviceMeetingList
	if err := focusDeviceGetJSON(ctx, bridge, "/meetings", url.Values{
		"limit":  {fmt.Sprint(limit)},
		"offset": {"0"},
	}, &response); err != nil {
		return nil, err
	}
	items := make([]Recording, 0, len(response.Meetings))
	for _, meeting := range response.Meetings {
		if strings.TrimSpace(meeting.ID) == "" {
			continue
		}
		items = append(items, deviceMeetingRecording(meeting, false))
	}
	return items, nil
}

func queryDeviceRecordingByID(parent context.Context, id string) (Recording, error) {
	bridge := bridges.NewDeviceDataBridgeFromEnv()
	if bridge == nil || bridge.Err != nil {
		return Recording{}, sql.ErrNoRows
	}
	ctx, cancel := context.WithTimeout(parent, focusDeviceOverallTimeout)
	defer cancel()
	var response focusDeviceMeetingDetail
	if err := focusDeviceGetJSON(ctx, bridge, "/meetings/"+url.PathEscape(id), nil, &response); err != nil {
		return Recording{}, sql.ErrNoRows
	}
	if strings.TrimSpace(response.Meeting.ID) != id {
		return Recording{}, sql.ErrNoRows
	}
	return deviceMeetingRecording(response.Meeting, true), nil
}

func deviceMeetingRecording(meeting focusDeviceMeeting, includeDetails bool) Recording {
	startedAt := focusDeviceMeetingTime(meeting.StartedAt, firstNonEmpty(meeting.UpdatedAt, meeting.CreatedAt))
	recording := Recording{
		ID:                strings.TrimSpace(meeting.ID),
		Title:             strings.TrimSpace(meeting.Title),
		Status:            strings.TrimSpace(meeting.Status),
		DurationSecs:      meeting.DurationSeconds,
		StartedAt:         startedAt,
		CreatedAt:         strings.TrimSpace(meeting.CreatedAt),
		UpdatedAt:         strings.TrimSpace(meeting.UpdatedAt),
		SpeakerCount:      meeting.SpeakerCount,
		AudioRef:          strings.TrimSpace(meeting.AudioRef),
		Summary:           meeting.Summary,
		PerSpeakerSummary: meeting.PerSpeakerSummary,
		Report:            meeting.Report,
	}
	if includeDetails {
		recording.Transcript = meeting.Transcript
	} else {
		recording.PerSpeakerSummary = ""
		recording.Report = ""
	}
	return recording
}
