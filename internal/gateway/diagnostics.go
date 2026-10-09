package gateway

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/user-no-found/FnPackKit/runtime/gatewaypolicy"
)

// writeFailure records only the shared validator's bounded audit vocabulary.
func writeFailure(w http.ResponseWriter, r *http.Request, rejection *gatewaypolicy.Rejection, status int) {
	id := gatewaypolicy.WriteFailure(w, r, rejection, status)
	fields := struct {
		Status    int    `json:"http_status"`
		RequestID string `json:"request_id"`
		gatewaypolicy.AuditFields
	}{status, id, gatewaypolicy.Audit(r, rejection)}
	data, _ := json.Marshal(fields)
	log.Printf("gateway_rejection %s", data)
}

// serveGatewayStatus proves only that HTTP policy and identity checks passed.
// It never asks the broker for a runtime or changes a user's terminal state.
func serveGatewayStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(struct {
		Schema int    `json:"schema"`
		Status string `json:"status"`
	}{1, "ready"})
}

// serveGatewayGrant issues only the application's fixed WebSocket target.
// No broker request, subprocess, or terminal input is involved.
func serveGatewayGrant(w http.ResponseWriter, r *http.Request, policy *gatewaypolicy.Policy, target string) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(body) != 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	grant, rejection := policy.IssueGrant(r, target)
	if rejection != nil {
		writeFailure(w, r, rejection, http.StatusForbidden)
		return
	}
	gatewaypolicy.WriteGrant(w, grant)
}
