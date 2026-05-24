package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

// configDiff describes the comparison result for one ConfigType between two
// runtimes. Used by GetRuntimeConfigDiff.
type configDiff struct {
	ConfigType string          `json:"config_type"`
	Left       json.RawMessage `json:"left,omitempty"`
	Right      json.RawMessage `json:"right,omitempty"`
	Status     string          `json:"status"` // "same", "different", "left_only", "right_only"
}

// initiateConfigWriteBody is the JSON body accepted by InitiateConfigWrite.
type initiateConfigWriteBody struct {
	Provider string                `json:"provider"`
	Configs  *daemon.ProviderConfigs `json:"configs"`
}

// configReadResultBody is the daemon's report for a config read request.
type configReadResultBody struct {
	Configs   *daemon.ProviderConfigs `json:"configs"`
	Supported *bool                   `json:"supported"`
	Error     string                  `json:"error"`
}

// configWriteResultBody is the daemon's report for a config write request.
type configWriteResultBody struct {
	Backups []string `json:"backups"`
	Error   string   `json:"error"`
}

// ---------------------------------------------------------------------------
// Runtime access helper
// ---------------------------------------------------------------------------

// lookupRuntime finds an agent runtime by ID and verifies the caller is a
// workspace member. Returns the runtime UUID and workspace ID on success, or
// writes an error response and returns ok=false.
func (h *Handler) lookupRuntime(w http.ResponseWriter, r *http.Request, runtimeID string) (rtUUID pgtype.UUID, workspaceID string, ok bool) {
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return pgtype.UUID{}, "", false
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return pgtype.UUID{}, "", false
	}

	wsID := uuidToString(rt.WorkspaceID)
	if _, ok := h.requireWorkspaceMember(w, r, wsID, "runtime not found"); !ok {
		return pgtype.UUID{}, "", false
	}

	return rt.ID, wsID, true
}

// ---------------------------------------------------------------------------
// User-facing handlers
// ---------------------------------------------------------------------------

// InitiateConfigRead creates a config read request for the daemon to process.
// POST /api/runtimes/{runtimeId}/config/read?provider=X
func (h *Handler) InitiateConfigRead(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	_, _, ok := h.lookupRuntime(w, r, runtimeID)
	if !ok {
		return
	}

	provider := r.URL.Query().Get("provider")
	if provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}

	req, err := h.ConfigReadStore.Create(r.Context(), runtimeID, provider)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue config read: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, req)
}

// GetConfigReadResult returns the result of a config read request.
// GET /api/runtimes/{runtimeId}/config/read/{requestId}
func (h *Handler) GetConfigReadResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	_, _, ok := h.lookupRuntime(w, r, runtimeID)
	if !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")
	req, err := h.ConfigReadStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if req == nil {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}

	writeJSON(w, http.StatusOK, req)
}

// GetRuntimeConfigs returns all parsed configs for a runtime.
// GET /api/runtimes/{runtimeId}/config
func (h *Handler) GetRuntimeConfigs(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, _, ok := h.lookupRuntime(w, r, runtimeID)
	if !ok {
		return
	}

	parsed, err := h.Queries.ListRuntimeConfigParsedByRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runtime configs: "+err.Error())
		return
	}

	if parsed == nil {
		parsed = []db.RuntimeConfigParsed{}
	}

	writeJSON(w, http.StatusOK, parsed)
}

// InitiateConfigWrite creates a config write request for the daemon.
// PUT /api/runtimes/{runtimeId}/config
func (h *Handler) InitiateConfigWrite(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	_, _, ok := h.lookupRuntime(w, r, runtimeID)
	if !ok {
		return
	}

	var body initiateConfigWriteBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}
	if body.Configs == nil {
		writeError(w, http.StatusBadRequest, "configs is required")
		return
	}

	req, err := h.ConfigWriteStore.Create(r.Context(), runtimeID, body.Provider, body.Configs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue config write: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, req)
}

// GetRuntimeConfigDiff compares parsed configs between two runtimes.
// GET /api/runtimes/{runtimeId}/config/diff?other_runtime_id=X
func (h *Handler) GetRuntimeConfigDiff(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, _, ok := h.lookupRuntime(w, r, runtimeID)
	if !ok {
		return
	}

	otherRuntimeID := r.URL.Query().Get("other_runtime_id")
	if otherRuntimeID == "" {
		writeError(w, http.StatusBadRequest, "other_runtime_id query parameter is required")
		return
	}

	otherUUID, ok := parseUUIDOrBadRequest(w, otherRuntimeID, "other_runtime_id")
	if !ok {
		return
	}

	leftParsed, err := h.Queries.ListRuntimeConfigParsedByRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list left runtime configs: "+err.Error())
		return
	}

	rightParsed, err := h.Queries.ListRuntimeConfigParsedByRuntime(r.Context(), otherUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list right runtime configs: "+err.Error())
		return
	}

	// Index both sides by config_type
	leftByType := make(map[string]db.RuntimeConfigParsed, len(leftParsed))
	for _, p := range leftParsed {
		leftByType[p.ConfigType] = p
	}

	rightByType := make(map[string]db.RuntimeConfigParsed, len(rightParsed))
	for _, p := range rightParsed {
		rightByType[p.ConfigType] = p
	}

	// Collect all config types from both sides
	seen := make(map[string]bool)
	allTypes := make([]string, 0)
	for _, p := range leftParsed {
		if !seen[p.ConfigType] {
			seen[p.ConfigType] = true
			allTypes = append(allTypes, p.ConfigType)
		}
	}
	for _, p := range rightParsed {
		if !seen[p.ConfigType] {
			seen[p.ConfigType] = true
			allTypes = append(allTypes, p.ConfigType)
		}
	}
	sort.Strings(allTypes)

	diffs := make([]configDiff, 0, len(allTypes))
	for _, ct := range allTypes {
		left, leftOK := leftByType[ct]
		right, rightOK := rightByType[ct]

		d := configDiff{ConfigType: ct}

		switch {
		case leftOK && !rightOK:
			d.Left = left.UnifiedSchema
			d.Status = "left_only"
		case !leftOK && rightOK:
			d.Right = right.UnifiedSchema
			d.Status = "right_only"
		case leftOK && rightOK:
			if string(left.UnifiedSchema) == string(right.UnifiedSchema) {
				d.Status = "same"
			} else {
				d.Left = left.UnifiedSchema
				d.Right = right.UnifiedSchema
				d.Status = "different"
			}
		}

		diffs = append(diffs, d)
	}

	writeJSON(w, http.StatusOK, diffs)
}

// ---------------------------------------------------------------------------
// Daemon-facing handlers
// ---------------------------------------------------------------------------

// ReportConfigReadResult accepts the daemon's result for a config read.
// POST /api/daemon/runtimes/{runtimeId}/config-read/{requestId}/result
func (h *Handler) ReportConfigReadResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")
	req, err := h.ConfigReadStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if req == nil || req.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}

	var body configReadResultBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Error != "" {
		if err := h.ConfigReadStore.Fail(r.Context(), requestID, body.Error); err != nil {
			slog.Error("config read store Fail failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
	} else {
		if err := h.ConfigReadStore.Complete(r.Context(), requestID, body.Configs); err != nil {
			slog.Error("config read store Complete failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist completion")
			return
		}

		// Fire-and-forget: persist raw configs and parse with LLM.
		if body.Configs != nil {
			go h.persistAndParseConfigs(r.Context(), runtimeID, body.Configs)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ReportConfigWriteResult accepts the daemon's result for a config write.
// POST /api/daemon/runtimes/{runtimeId}/config-write/{requestId}/result
func (h *Handler) ReportConfigWriteResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")
	req, err := h.ConfigWriteStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if req == nil || req.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}

	var body configWriteResultBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Error != "" {
		if err := h.ConfigWriteStore.Fail(r.Context(), requestID, body.Error); err != nil {
			slog.Error("config write store Fail failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
	} else {
		if err := h.ConfigWriteStore.Complete(r.Context(), requestID, body.Backups); err != nil {
			slog.Error("config write store Complete failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist completion")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// persistAndParseConfigs stores raw configs from a successful read and runs
// LLM parsing to produce unified schemas. Intended to be called as a
// fire-and-forget goroutine from ReportConfigReadResult.
func (h *Handler) persistAndParseConfigs(ctx context.Context, runtimeID string, configs *daemon.ProviderConfigs) {
	runtimeUUID, err := util.ParseUUID(runtimeID)
	if err != nil {
		slog.Error("persistAndParseConfigs: invalid runtime UUID", "runtime_id", runtimeID, "error", err)
		return
	}

	toolVersion := configs.Version
	if toolVersion == "" {
		toolVersion = "unknown"
	}

	for _, ct := range AllConfigTypes {
		raw := configTypeRaw(configs, ct)
		if raw == "" {
			continue
		}

		hash := sha256Hex(raw)

		snap, err := h.Queries.InsertRuntimeConfigSnapshot(ctx, db.InsertRuntimeConfigSnapshotParams{
			RuntimeID:    runtimeUUID,
			ConfigType:   string(ct),
			Provider:     configs.Provider,
			RawContent:   []byte(raw),
			ToolVersion:  toolVersion,
			ContentHash:  hash,
			Success:      true,
			ErrorMessage: "",
		})
		if err != nil {
			slog.Error("persistAndParseConfigs: InsertRuntimeConfigSnapshot failed",
				"config_type", ct, "error", err)
			continue
		}

		// Stub: store raw content as unified_schema for now.
		// TODO: Replace with actual LLM parsing.
		_, err = h.Queries.UpsertRuntimeConfigParsed(ctx, db.UpsertRuntimeConfigParsedParams{
			RuntimeID:     runtimeUUID,
			ConfigType:    string(ct),
			UnifiedSchema: []byte(raw),
			SnapshotID:    snap.ID,
			ParsedBy:      "stub",
			SchemaVersion: 1,
			UnknownKeys:   nil,
			Warnings:      nil,
		})
		if err != nil {
			slog.Error("persistAndParseConfigs: UpsertRuntimeConfigParsed failed",
				"config_type", ct, "error", err)
		}
	}
}

// sha256Hex computes the SHA-256 hex digest of a string.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
