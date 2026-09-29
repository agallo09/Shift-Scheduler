package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestApprovalPageRendersSubmittedSchedule(t *testing.T) {
	page, err := buildApprovalPageData(map[string]scheduleSubmission{
		"student1": {
			EmploymentType: "part-time",
			Status:         "pending",
			Availability: map[string][]string{
				"Monday": slotRange(8*60, 18),
			},
			SubmittedAt: time.Date(2026, time.May, 15, 9, 30, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	tmpl, err := template.ParseFiles("templates/approval.html")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	if err := tmpl.Execute(response, page); err != nil {
		t.Fatal(err)
	}

	body := response.Body.String()
	for _, expected := range []string{
		"1 pending reviews",
		"student1",
		"part-time",
		"3.0",
		"Monday:",
		"8:00 AM - 11:00 AM",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("approval page does not contain %q", expected)
		}
	}
	if strings.Contains(body, "Alex Morgan") {
		t.Error("approval page still contains the hard-coded sample student")
	}
}

func TestStudentSchedulePageRendersSavedDeniedSchedule(t *testing.T) {
	tmpl, err := template.ParseFiles("templates/schedule.html")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	page := studentPageData{
		HasSubmission: true,
		EmploymentType: "part-time",
		ReviewStatus:   "denied",
		DenialReason:   "Please add another day.",
		SelectedSlots:  []studentSlot{{Day: "Monday", Minute: 8 * 60}},
	}
	if err := tmpl.Execute(response, page); err != nil {
		t.Fatal(err)
	}

	body := response.Body.String()
	for _, expected := range []string{
		`data-review-status="denied"`,
		`data-day="Monday" data-minute="480"`,
		"Please add another day.",
		`input.checked = input.value === "part-time"`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("student page does not contain %q", expected)
		}
	}
}

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

func TestHandleReviewScheduleStoresDecision(t *testing.T) {
	const student = "review-test-student"
	scheduleMu.Lock()
	previous, existed := schedules[student]
	schedules[student] = scheduleSubmission{
		EmploymentType: "part-time",
		Availability:   map[string][]string{"Monday": slotRange(8*60, 18)},
		Status:         "pending",
	}
	scheduleMu.Unlock()
	t.Cleanup(func() {
		scheduleMu.Lock()
		if existed {
			schedules[student] = previous
		} else {
			delete(schedules, student)
		}
		scheduleMu.Unlock()
	})

	request := httptest.NewRequest(http.MethodPost, "/review-schedule", strings.NewReader(
		"student="+student+"&decision=denied&reason=Please+add+weekday+availability",
	))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: "username", Value: "admin1"})
	response := httptest.NewRecorder()

	handleReviewSchedule(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusSeeOther, response.Body.String())
	}
	scheduleMu.RLock()
	reviewed := schedules[student]
	scheduleMu.RUnlock()
	if reviewed.Status != "denied" || reviewed.DenialReason != "Please add weekday availability" {
		t.Fatalf("reviewed schedule = %+v, want denied with reason", reviewed)
	}
}

func TestDeniedScheduleCanBeResubmitted(t *testing.T) {
	const student = "student1"
	scheduleMu.Lock()
	previous, existed := schedules[student]
	schedules[student] = scheduleSubmission{
		EmploymentType: "part-time",
		Availability:   map[string][]string{"Monday": slotRange(8*60, 18)},
		Status:         "denied",
		DenialReason:   "Please make a change",
	}
	scheduleMu.Unlock()
	t.Cleanup(func() {
		scheduleMu.Lock()
		if existed {
			schedules[student] = previous
		} else {
			delete(schedules, student)
		}
		scheduleMu.Unlock()
	})

	requestBody, err := json.Marshal(submission("part-time", map[string][]string{
		"Tuesday": slotRange(8*60, 18),
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/submit-schedule", strings.NewReader(string(requestBody)))
	request.AddCookie(&http.Cookie{Name: "username", Value: student})
	response := httptest.NewRecorder()
	handleScheduleSubmission(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	scheduleMu.RLock()
	resubmitted := schedules[student]
	scheduleMu.RUnlock()
	if resubmitted.Status != "pending" || resubmitted.DenialReason != "" {
		t.Fatalf("resubmitted schedule = %+v, want pending without denial reason", resubmitted)
	}
	if _, exists := resubmitted.Availability["Tuesday"]; !exists {
		t.Fatal("resubmitted availability was not saved")
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
