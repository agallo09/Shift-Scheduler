package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleScheduleSubmissionReturnsJSON(t *testing.T) {
	requestBody, err := json.Marshal(submission("part-time", map[string][]string{
		"Monday": slotRange(8*60, 18),
	}))
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/submit-schedule", strings.NewReader(string(requestBody)))
	request.AddCookie(&http.Cookie{Name: "username", Value: "student1"})
	response := httptest.NewRecorder()

	handleScheduleSubmission(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}

	var result struct {
		Eligible bool   `json:"eligible"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if !result.Eligible || result.Message == "" {
		t.Fatalf("response = %+v, want eligible response with message", result)
	}
}

func TestValidateScheduleSubmission(t *testing.T) {
	tests := []struct {
		name       string
		submission scheduleSubmission
		wantErr    bool
	}{
		{
			name:       "part time minimum",
			submission: submission("part-time", map[string][]string{"Monday": slotRange(8*60, 18)}),
		},
		{
			name:       "part time maximum",
			submission: submission("part-time", map[string][]string{"Monday": slotRange(8*60, 54), "Tuesday": slotRange(8*60, 48), "Wednesday": slotRange(8*60, 18)}),
		},
		{
			name:       "part time weekly maximum exceeded",
			submission: submission("part-time", map[string][]string{"Monday": slotRange(8*60, 54), "Tuesday": slotRange(8*60, 48), "Wednesday": slotRange(8*60, 19)}),
			wantErr:    true,
		},
		{
			name:       "full time weekly minimum",
			submission: submission("full-time", map[string][]string{"Monday": slotRange(8*60, 54), "Tuesday": slotRange(8*60, 48), "Wednesday": slotRange(8*60, 18)}),
		},
		{
			name:       "full time weekly maximum",
			submission: submission("full-time", map[string][]string{"Monday": slotRange(8*60, 54), "Tuesday": slotRange(8*60, 54), "Wednesday": slotRange(8*60, 54), "Thursday": slotRange(8*60, 54), "Friday": slotRange(8*60, 24)}),
		},
		{
			name:       "full time weekly maximum exceeded",
			submission: submission("full-time", map[string][]string{"Monday": slotRange(8*60, 54), "Tuesday": slotRange(8*60, 54), "Wednesday": slotRange(8*60, 54), "Thursday": slotRange(8*60, 54), "Friday": slotRange(8*60, 25)}),
			wantErr:    true,
		},
		{
			name:       "full time below weekly minimum",
			submission: submission("full-time", map[string][]string{"Monday": slotRange(8*60, 54), "Tuesday": slotRange(8*60, 42), "Wednesday": slotRange(8*60, 18)}),
			wantErr:    true,
		},
		{
			name:       "shift shorter than three hours",
			submission: submission("part-time", map[string][]string{"Monday": slotRange(8*60, 17), "Tuesday": slotRange(8*60, 18)}),
			wantErr:    true,
		},
		{
			name:       "two short shifts cannot add up to minimum",
			submission: submission("part-time", map[string][]string{"Monday": append(slotRange(8*60, 9), slotRange(10*60, 9)...)}),
			wantErr:    true,
		},
		{
			name: "daily maximum is nine hours",
			submission: submission("part-time", map[string][]string{
				"Monday": append(slotRange(8*60, 54), slotRange(17*60, 1)...),
			}),
			wantErr: true,
		},
		{
			name:       "invalid employment type",
			submission: submission("contract", map[string][]string{"Monday": slotRange(8*60, 18)}),
			wantErr:    true,
		},
		{
			name:       "unknown day",
			submission: submission("part-time", map[string][]string{"Saturday": slotRange(8*60, 18)}),
			wantErr:    true,
		},
		{
			name:       "time not on a ten minute boundary",
			submission: submission("part-time", map[string][]string{"Monday": append([]string{"08:05"}, slotRange(8*60+10, 17)...)}),
			wantErr:    true,
		},
		{
			name:       "time outside schedule",
			submission: submission("part-time", map[string][]string{"Monday": append([]string{"18:00"}, slotRange(8*60, 18)...)}),
			wantErr:    true,
		},
		{
			name:       "duplicate time",
			submission: submission("part-time", map[string][]string{"Monday": append(slotRange(8*60, 18), "08:00")}),
			wantErr:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateScheduleSubmission(test.submission)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateScheduleSubmission() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func submission(employmentType string, availability map[string][]string) scheduleSubmission {
	return scheduleSubmission{
		EmploymentType: employmentType,
		Availability:   availability,
	}
}

func slotRange(startMinute, count int) []string {
	slots := make([]string, count)
	for index := range count {
		minute := startMinute + index*10
		slots[index] = fmt.Sprintf("%02d:%02d", minute/60, minute%60)
	}
	return slots
}
