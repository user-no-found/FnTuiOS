package service

import (
	"encoding/json"
	"net/http"
	"strings"
)

const imageAuthoritySession = "image-authority"

// browserImageAuthorityFixture owns only disposable session fixtures, never
// host users or installed app services. Its fixed commands leave no arbitrary
// command execution or client-selected session names in the HTTP fixture.
func (fixture *containerFixture) browserImageAuthorityFixture(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/__fixture_authority_") {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	user := fixture.users[0]
	encode := func(value any) { _ = json.NewEncoder(w).Encode(value) }
	if r.URL.Path == "/__fixture_authority_clients" && r.Method == http.MethodGet {
		_, _ = w.Write([]byte(fixture.cli(user, "list-clients", "--json")))
		return true
	}
	if r.URL.Path == "/__fixture_authority_panes" && r.Method == http.MethodGet {
		_, _ = w.Write([]byte(fixture.cli(user, "list-windows", "-s", imageAuthoritySession, "--json")))
		return true
	}
	if r.URL.Path == "/__fixture_authority_capture" && r.Method == http.MethodGet {
		pane := r.URL.Query().Get("pane")
		if !fixturePaneID.MatchString(pane) {
			http.Error(w, "pane", 400)
			return true
		}
		encode(map[string]string{"text": fixture.cli(user, "capture-pane", "-s", imageAuthoritySession, "-w", pane, "--scrollback")})
		return true
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method", 405)
		return true
	}
	switch r.URL.Path {
	case "/__fixture_authority_prepare":
		switched := fixture.cli(user, "switch-session", "--create", "-s", "web", imageAuthoritySession, "--json")
		created := make([]string, 0, 2)
		for _, name := range []string{"authority-a", "authority-b"} {
			var reply struct {
				WindowID string `json:"window_id"`
			}
			raw := fixture.cli(user, "new-window", name, "-s", imageAuthoritySession, "--json", "--", "/bin/bash", "-c",
				"printf 'FN_AUTHORITY_READY\\n'; IFS= read -r image; printf 'FN_AUTHORITY_SUBMITTED\\n'; sleep 300")
			if err := json.Unmarshal([]byte(raw), &reply); err != nil || reply.WindowID == "" {
				http.Error(w, "new pane response", 500)
				return true
			}
			created = append(created, reply.WindowID)
		}
		var listed struct {
			Windows []struct {
				ID string `json:"window_id"`
			} `json:"windows"`
		}
		if err := json.Unmarshal([]byte(fixture.cli(user, "list-windows", "-s", imageAuthoritySession, "--json")), &listed); err != nil {
			http.Error(w, "pane list response", 500)
			return true
		}
		for _, pane := range listed.Windows {
			if pane.ID != created[0] && pane.ID != created[1] {
				fixture.cli(user, "close-window", pane.ID, "-s", imageAuthoritySession, "--json")
			}
		}
		fixture.cli(user, "focus-window", created[0], "-s", imageAuthoritySession, "--json")
		encode(map[string]any{"first": created[0], "second": created[1], "switch": json.RawMessage(switched)})
	case "/__fixture_authority_roundtrip":
		away := fixture.cli(user, "switch-session", "-s", imageAuthoritySession, "web", "--json")
		back := fixture.cli(user, "switch-session", "-s", "web", imageAuthoritySession, "--json")
		encode(map[string]any{"away": json.RawMessage(away), "back": json.RawMessage(back)})
	case "/__fixture_authority_restore":
		_, _ = w.Write([]byte(fixture.cli(user, "switch-session", "-s", imageAuthoritySession, "web", "--json")))
	default:
		http.NotFound(w, r)
	}
	return true
}
