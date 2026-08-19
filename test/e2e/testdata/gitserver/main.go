package main

import (
	"log"
	"net/http"
	"net/http/cgi"
	"os/exec"
)

func main() {
	backend, err := exec.LookPath("git-http-backend")
	if err != nil {
		log.Fatalf("find git-http-backend: %v", err)
	}

	http.Handle("/", &cgi.Handler{
		Path: backend,
		Env: []string{
			"GIT_HTTP_EXPORT_ALL=1",
			"GIT_PROJECT_ROOT=/srv",
		},
	})

	log.Print("serving git HTTP fixture on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}
