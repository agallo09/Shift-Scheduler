package main

import (
	"html/template"
	"log"
	"net/http"
)

// roles
var users = map[string]string{
	"student": "student",
	"admin":   "admin",
}

func main() {
	//initial login
	log.Println("Starting server at port 8080")
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
	http.HandleFunc("/student", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:  "username",
			Value: "student1",
			Path:  "/",
		})
		err := tmpl1.Execute(w, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	//admin button
	tmpl2 := template.Must(template.ParseFiles("templates/approval.html"))
	http.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:  "username",
			Value: "admin1",
			Path:  "/",
		})
		err := tmpl2.Execute(w, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	log.Fatal(http.ListenAndServe("localhost:8080", nil))

}
