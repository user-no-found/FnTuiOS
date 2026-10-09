package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

var fixturePaneID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

// browserImageFixture exposes only disposable-container observations and pane actions.
func (fixture *containerFixture) browserImageFixture(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/__fixture_image_") {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	user := fixture.users[0]
	switch r.URL.Path {
	case "/__fixture_image_panes":
		if r.Method != http.MethodGet {
			http.Error(w, "method", 405)
			return true
		}
		w.Write([]byte(fixture.cli(user, "list-windows", "-s", "web", "--json")))
	case "/__fixture_image_capture":
		pane := r.URL.Query().Get("pane")
		if r.Method != http.MethodGet || !fixturePaneID.MatchString(pane) {
			http.Error(w, "request", 400)
			return true
		}
		json.NewEncoder(w).Encode(map[string]string{"text": fixture.cli(user, "capture-pane", "-s", "web", "-w", pane, "--scrollback")})
	case "/__fixture_image_action":
		action, pane := r.URL.Query().Get("action"), r.URL.Query().Get("pane")
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return true
		}
		var args []string
		switch action {
		case "keys":
			args = []string{"new-window", "image-keys", "-s", "web", "--json", "--", "/bin/bash", "-c", "stty -echo -icanon min 1 time 0; printf '\\033[>3uFN_KEYS_READY\\n'; sleep 300"}
		case "reader":
			args = []string{"new-window", "image-reader", "-s", "web", "--json", "--", "/bin/bash", "-c", "printf 'FN_IMAGE_READY\\n'; IFS= read -r image; printf 'FN_IMAGE_SUBMITTED\\n'; sleep 300"}
		case "new":
			args = []string{"new-window", "image-fixture", "-s", "web", "--json"}
		case "focus", "close":
			if !fixturePaneID.MatchString(pane) {
				http.Error(w, "pane", 400)
				return true
			}
			args = []string{action + "-window", pane, "-s", "web", "--json"}
		default:
			http.Error(w, "action", 400)
			return true
		}
		w.Write([]byte(fixture.cli(user, args...)))
	case "/__fixture_image_files":
		if r.Method != http.MethodGet {
			http.Error(w, "method", 405)
			return true
		}
		files := []map[string]any{}
		for _, owner := range fixture.users {
			directory := filepath.Join(fixture.cfg.RunDir, strconv.Itoa(int(owner.UID)), "tuios", "paste")
			entries, err := os.ReadDir(directory)
			if err != nil && !os.IsNotExist(err) {
				http.Error(w, "read directory", 500)
				return true
			}
			for _, entry := range entries {
				info, err := entry.Info()
				if err != nil || !info.Mode().IsRegular() {
					http.Error(w, "nonregular paste", 500)
					return true
				}
				data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
				if err != nil {
					http.Error(w, "read file", 500)
					return true
				}
				sum := sha256.Sum256(data)
				stat := info.Sys().(*syscall.Stat_t)
				files = append(files, map[string]any{"path": filepath.Join(directory, entry.Name()),
					"mode": fmt.Sprintf("%04o", info.Mode().Perm()), "uid": stat.Uid, "size": info.Size(), "sha256": hex.EncodeToString(sum[:])})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"files": files})
	default:
		http.NotFound(w, r)
	}
	return true
}
