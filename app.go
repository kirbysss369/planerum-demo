package hours

import (
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type App struct {
	Service  Service
	Jobs     JobQueue
	Exporter Exporter
	Token    string
	Embed    bool
	Demo     bool
}

func (a App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/hours/validate", Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /{$}", a.home)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.Handle("POST /v1/agent", a.auth(http.HandlerFunc(a.agent)))
	mux.Handle("POST /v1/references", a.auth(http.HandlerFunc(a.references)))
	mux.Handle("GET /artifacts/{programID}/{revision}/{filename}", a.auth(http.HandlerFunc(a.artifact)))
	return mux
}

func (a App) artifact(w http.ResponseWriter, r *http.Request) {
	id, revision, name := r.PathValue("programID"), r.PathValue("revision"), r.PathValue("filename")
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != 12 || !slices.Contains(artifactNames, name) || !strings.HasPrefix(revision, "revision-") {
		http.NotFound(w, r)
		return
	}
	version, err := strconv.Atoi(strings.TrimPrefix(revision, "revision-"))
	if err != nil || version < 1 || revision != "revision-"+strconv.Itoa(version) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(a.Exporter.Root, id, revision, name)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}

func (a App) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Token != "" && r.Header.Get("Authorization") != "Bearer "+a.Token {
			writeAgent(w, http.StatusUnauthorized, AgentResponse{Error: "access token is required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
