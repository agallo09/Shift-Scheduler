package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

type scheduleSubmission struct {
	EmploymentType string              `json:"employmentType"`
	Availability   map[string][]string `json:"availability"`
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
	scheduleMu.Lock()
	schedules[cookie.Value] = submission
	scheduleMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"eligible":       true,
		"employmentType": submission.EmploymentType,
		"message":        fmt.Sprintf("Availability submitted. You meet the %s availability requirements.", submission.EmploymentType),
	}); err != nil {
		http.Error(w, "Could not write submission response.", http.StatusInternalServerError)
	}
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
