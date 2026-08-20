package main

import (
	"log"
	"net/http"
	"net/http/cgi" // #nosec G504 -- this test-only fixture intentionally serves git-http-backend through CGI.
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	execPath, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		log.Fatalf("find git exec path: %v", err)
	}
	backend, err := findGitHTTPBackend(strings.TrimSpace(string(execPath)))
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	http.Handle("/", &cgi.Handler{
		Path:   backend,
		Stderr: os.Stderr,
		Env: []string{
			"GIT_HTTP_EXPORT_ALL=1",
			"GIT_PROJECT_ROOT=/srv",
		},
	})

	log.Print("serving git HTTP fixture on :8080")
	server := &http.Server{
		Addr:              ":8080",
		Handler:           http.DefaultServeMux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func findGitHTTPBackend(execPath string) (string, error) {
	candidates := []string{
		filepath.Join(execPath, "git-http-backend"),
		"/usr/lib/git-core/git-http-backend",
		"/usr/libexec/git-core/git-http-backend",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	var found string
	_ = filepath.WalkDir("/usr", func(candidate string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Base(candidate) != "git-http-backend" {
			return nil
		}
		found = candidate
		return filepath.SkipAll
	})
	if found != "" {
		return found, nil
	}

	return "", os.ErrNotExist
}
