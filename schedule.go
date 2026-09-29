package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type scheduleSubmission struct {
	EmploymentType string              `json:"employmentType"`
	Availability   map[string][]string `json:"availability"`
	SubmittedAt    time.Time           `json:"-"`
	Status         string              `json:"-"`
	DenialReason   string              `json:"-"`
}

type approvalPageData struct {
	PendingCount int
	Submissions  []approvalSubmission
}

type studentPageData struct {
	HasSubmission  bool
	EmploymentType string
	ReviewStatus   string
	DenialReason   string
	SelectedSlots  []studentSlot
}

type studentSlot struct {
	Day    string
	Minute int
}

type approvalSubmission struct {
	StudentName    string
	EmploymentType string
	SubmittedAt    string
	TotalHours     string
	DayCount       int
	Days           []approvalDay
	Status         string
	DenialReason   string
}

type approvalDay struct {
	Name   string
	Ranges []string
}

var (
	scheduleMu sync.RWMutex
	schedules  = make(map[string]scheduleSubmission)
)

var scheduleDays = []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday"}

func handleScheduleSubmission(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeScheduleError(w, http.StatusMethodNotAllowed, "Use POST to submit availability.")
		return
	}

	cookie, err := r.Cookie("username")
	if err != nil || cookie.Value != "student1" {
		writeScheduleError(w, http.StatusUnauthorized, "Sign in as a student before submitting availability.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var submission scheduleSubmission
	if err := decoder.Decode(&submission); err != nil {
		writeScheduleError(w, http.StatusBadRequest, "Invalid availability request.")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeScheduleError(w, http.StatusBadRequest, "Request must contain a single JSON object.")
		return
	}
	if err := validateScheduleSubmission(submission); err != nil {
		writeScheduleError(w, http.StatusBadRequest, err.Error())
		return
	}

	for day := range submission.Availability {
		sort.Strings(submission.Availability[day])
	}
	submission.SubmittedAt = time.Now()
	scheduleMu.Lock()
	if previous, exists := schedules[cookie.Value]; exists && previous.Status != "denied" {
		scheduleMu.Unlock()
		writeScheduleError(w, http.StatusConflict, "A schedule is already awaiting review or has been approved.")
		return
	}
	submission.Status = "pending"
	schedules[cookie.Value] = submission
	scheduleMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"eligible":       true,
		"employmentType": submission.EmploymentType,
		"status":         submission.Status,
		"message":        "Schedule submitted and pending approval.",
	}); err != nil {
		http.Error(w, "Could not write submission response.", http.StatusInternalServerError)
	}
}

func handleReviewSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Use POST to review a schedule.", http.StatusMethodNotAllowed)
		return
	}
	cookie, err := r.Cookie("username")
	if err != nil || cookie.Value != "admin1" {
		http.Error(w, "Sign in as an admin to review schedules.", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid review request.", http.StatusBadRequest)
		return
	}

	student := r.FormValue("student")
	decision := r.FormValue("decision")
	reason := strings.TrimSpace(r.FormValue("reason"))
	if student == "" || (decision != "approved" && decision != "denied") {
		http.Error(w, "Choose a valid schedule and decision.", http.StatusBadRequest)
		return
	}
	if len(reason) > 500 {
		http.Error(w, "The review note must be 500 characters or fewer.", http.StatusBadRequest)
		return
	}
	if decision == "denied" && reason == "" {
		http.Error(w, "A reason is required when denying a schedule.", http.StatusBadRequest)
		return
	}

	scheduleMu.Lock()
	submission, exists := schedules[student]
	if !exists {
		scheduleMu.Unlock()
		http.Error(w, "The submitted schedule was not found.", http.StatusNotFound)
		return
	}
	if submission.Status != "pending" {
		scheduleMu.Unlock()
		http.Error(w, "This schedule has already been reviewed.", http.StatusConflict)
		return
	}
	submission.Status = decision
	if decision == "denied" {
		submission.DenialReason = reason
	} else {
		submission.DenialReason = ""
	}
	schedules[student] = submission
	scheduleMu.Unlock()

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func handleStudentStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeScheduleError(w, http.StatusMethodNotAllowed, "Use GET to check schedule status.")
		return
	}
	cookie, err := r.Cookie("username")
	if err != nil || cookie.Value != "student1" {
		writeScheduleError(w, http.StatusUnauthorized, "Sign in as a student to check schedule status.")
		return
	}

	scheduleMu.RLock()
	submission, exists := schedules[cookie.Value]
	scheduleMu.RUnlock()
	status := "not-submitted"
	reason := ""
	if exists {
		status = submission.Status
		reason = submission.DenialReason
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": status, "denialReason": reason}); err != nil {
		http.Error(w, "Could not write schedule status.", http.StatusInternalServerError)
	}
}

func currentStudentPageData(student string) (studentPageData, error) {
	page := studentPageData{ReviewStatus: "not-submitted"}
	scheduleMu.RLock()
	defer scheduleMu.RUnlock()
	submission, exists := schedules[student]
	if !exists {
		return page, nil
	}
	page.HasSubmission = true
	page.EmploymentType = submission.EmploymentType
	page.ReviewStatus = submission.Status
	page.DenialReason = submission.DenialReason
	for _, day := range scheduleDays {
		for _, slot := range submission.Availability[day] {
			parsed, err := time.Parse("15:04", slot)
			if err != nil {
				return studentPageData{}, fmt.Errorf("load saved availability for %s: invalid time %q: %w", day, slot, err)
			}
			page.SelectedSlots = append(page.SelectedSlots, studentSlot{
				Day:    day,
				Minute: parsed.Hour()*60 + parsed.Minute(),
			})
		}
	}
	return page, nil
}

func buildApprovalPageData(saved map[string]scheduleSubmission) (approvalPageData, error) {
	page := approvalPageData{}
	for student, submission := range saved {
		view := approvalSubmission{
			StudentName:    student,
			EmploymentType: submission.EmploymentType,
			SubmittedAt:    submission.SubmittedAt.Format("Jan 2, 2006 3:04 PM"),
			Status:         submission.Status,
			DenialReason:   submission.DenialReason,
		}
		totalSlots := 0
		for _, day := range scheduleDays {
			slots := submission.Availability[day]
			if len(slots) == 0 {
				continue
			}
			totalSlots += len(slots)
			view.DayCount++
			ranges, err := availabilityRanges(slots)
			if err != nil {
				return approvalPageData{}, fmt.Errorf("build schedule for %s: %w", student, err)
			}
			view.Days = append(view.Days, approvalDay{
				Name:   day,
				Ranges: ranges,
			})
		}
		view.TotalHours = fmt.Sprintf("%.1f", float64(totalSlots)/6)
		page.Submissions = append(page.Submissions, view)
		if submission.Status == "pending" {
			page.PendingCount++
		}
	}
	sort.Slice(page.Submissions, func(i, j int) bool {
		return page.Submissions[i].StudentName < page.Submissions[j].StudentName
	})
	return page, nil
}

func currentApprovalPageData() (approvalPageData, error) {
	scheduleMu.RLock()
	defer scheduleMu.RUnlock()
	return buildApprovalPageData(schedules)
}

func availabilityRanges(slots []string) ([]string, error) {
	minutes := make([]int, 0, len(slots))
	for _, slot := range slots {
		parsed, err := time.Parse("15:04", slot)
		if err != nil {
			return nil, fmt.Errorf("invalid saved availability time %q: %w", slot, err)
		}
		minutes = append(minutes, parsed.Hour()*60+parsed.Minute())
	}
	sort.Ints(minutes)

	var ranges []string
	for index := 0; index < len(minutes); {
		start := minutes[index]
		end := start + 10
		index++
		for index < len(minutes) && minutes[index] == end {
			end += 10
			index++
		}
		ranges = append(ranges, fmt.Sprintf("%s - %s", formatClockTime(start), formatClockTime(end)))
	}
	return ranges, nil
}

func formatClockTime(minute int) string {
	hour := minute / 60
	return time.Date(2000, time.January, 1, hour, minute%60, 0, 0, time.UTC).Format("3:04 PM")
}

func validateScheduleSubmission(submission scheduleSubmission) error {
	minimumWeeklySlots, maximumWeeklySlots := 120, 240
	switch submission.EmploymentType {
	case "part-time":
		minimumWeeklySlots, maximumWeeklySlots = 18, 120
	case "full-time":
	default:
		return fmt.Errorf("Choose part-time or full-time before submitting.")
	}

	availability := submission.Availability
	if len(availability) == 0 {
		return fmt.Errorf("Select at least one available time.")
	}

	allowedDays := make(map[string]struct{}, len(scheduleDays))
	for _, day := range scheduleDays {
		allowedDays[day] = struct{}{}
	}
	totalSlots := 0
	for day, slots := range availability {
		if _, ok := allowedDays[day]; !ok {
			return fmt.Errorf("%q is not a valid schedule day.", day)
		}
		seen := make(map[string]struct{}, len(slots))
		minutes := make([]int, 0, len(slots))
		for _, slot := range slots {
			parsed, err := time.Parse("15:04", slot)
			if err != nil || parsed.Format("15:04") != slot {
				return fmt.Errorf("%q is not a valid time.", slot)
			}
			minute := parsed.Hour()*60 + parsed.Minute()
			if minute < 8*60 || minute >= 18*60 || (minute-8*60)%10 != 0 {
				return fmt.Errorf("%q is outside the schedule or not on a 10-minute boundary.", slot)
			}
			if _, exists := seen[slot]; exists {
				return fmt.Errorf("%q is duplicated for %s.", slot, day)
			}
			seen[slot] = struct{}{}
			minutes = append(minutes, minute)
		}
		sort.Ints(minutes)
		if len(minutes) > 54 {
			return fmt.Errorf("%s exceeds the 9-hour daily maximum.", day)
		}
		for index := 0; index < len(minutes); {
			runSlots := 1
			for index+runSlots < len(minutes) && minutes[index+runSlots] == minutes[index+runSlots-1]+10 {
				runSlots++
			}
			if runSlots < 18 {
				return fmt.Errorf("%s has an availability block shorter than 3 hours.", day)
			}
			index += runSlots
		}
		totalSlots += len(minutes)
	}
	if totalSlots < minimumWeeklySlots {
		return fmt.Errorf("Weekly availability is below the minimum for %s.", submission.EmploymentType)
	}
	if totalSlots > maximumWeeklySlots {
		return fmt.Errorf("Weekly availability exceeds the maximum for %s.", submission.EmploymentType)
	}
	return nil
}

func writeScheduleError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		http.Error(w, "Could not write error response.", http.StatusInternalServerError)
	}
}
