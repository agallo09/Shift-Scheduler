package main

import (
	"html/template"
	"log"
	"net/http"
)

// roles
var users = map[string]string{
	"student1": "student",
	"admin1":   "admin",
}

func main() {
	//initial login
	log.Println("Starting server at port 8080")
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	//home page load login.html
	tmpl := template.Must(template.ParseFiles("templates/login.html"))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		err := tmpl.Execute(w, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	//student login button
	tmpl1 := template.Must(template.ParseFiles("templates/schedule.html"))
	http.HandleFunc("/student-status", handleStudentStatus)
	http.HandleFunc("/student", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:  "username",
			Value: "student1",
			Path:  "/",
		})
		page, err := currentStudentPageData("student1")
		if err != nil {
			log.Printf("Could not load student schedule: %v", err)
			http.Error(w, "Could not load your saved schedule.", http.StatusInternalServerError)
			return
		}
		err = tmpl1.Execute(w, page)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	http.HandleFunc("/submit-schedule", handleScheduleSubmission)
	http.HandleFunc("/review-schedule", handleReviewSchedule)
	//admin button
	tmpl2 := template.Must(template.ParseFiles("templates/approval.html"))
	http.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:  "username",
			Value: "admin1",
			Path:  "/",
		})
		page, err := currentApprovalPageData()
		if err != nil {
			log.Printf("Could not build approval dashboard: %v", err)
			http.Error(w, "Could not load submitted schedules.", http.StatusInternalServerError)
			return
		}
		err = tmpl2.Execute(w, page)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	log.Fatal(http.ListenAndServe("localhost:8080", nil))

}
