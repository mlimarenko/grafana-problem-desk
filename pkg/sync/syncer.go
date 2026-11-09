package sync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	stdsync "sync"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/sanitizer"
	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
)

const (
	maxAlertsResponseBytes = 16 * 1024 * 1024 // 16 MiB strict limit
	defaultHTTPTimeout     = 30 * time.Second
)

// Syncer handles synchronized alert snapshot fetching from Grafana Alertmanager.
type Syncer struct {
	client *http.Client
	mu     stdsync.Mutex
}

// NewSyncer constructs a Syncer with strict security controls:
// - Disallows redirects to protect bearer tokens
// - Verified TLS
// - Bounded timeouts
func NewSyncer(customClient *http.Client) *Syncer {
	if customClient != nil {
		return &Syncer{client: customClient}
	}
	client := &http.Client{
		Timeout: defaultHTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return errors.New("redirects are forbidden for bearer token requests")
		},
	}
	return &Syncer{client: client}
}

// ValidateSourceURL checks that rawURL is a clean http/https URL with host,
// and without userinfo, query parameters or fragments.
func ValidateSourceURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("grafanaUrl is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("invalid grafanaUrl")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("grafanaUrl scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("grafanaUrl host cannot be empty")
	}
	if u.User != nil {
		return nil, errors.New("grafanaUrl must not contain userinfo")
	}
	if u.RawQuery != "" {
		return nil, errors.New("grafanaUrl must not contain query parameters")
	}
	if u.Fragment != "" {
		return nil, errors.New("grafanaUrl must not contain fragment")
	}
	return u, nil
}

type alertmanagerAlert struct {
	Fingerprint  string            `json:"fingerprint"`
	StartsAt     string            `json:"startsAt"`
	EndsAt       string            `json:"endsAt"`
	Status       alertStatus       `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	GeneratorURL string            `json:"generatorURL"`
}

type alertStatus struct {
	State string `json:"state"`
}

type orgResponse struct {
	ID int64 `json:"id"`
}

func decodeCompleteJSON(reader io.Reader, limit int64, value any) error {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > limit {
		return errors.New("source response exceeded the size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("unexpected trailing content in source response")
	}
	return nil
}

// joinURLPath joins a base URL with an endpoint path while preserving
// any configured subpath (e.g. https://host/grafana/ -> https://host/grafana/api/org).
func joinURLPath(base *url.URL, endpoint string) string {
	u := *base
	basePath := strings.TrimRight(u.Path, "/")
	cleanEndpoint := "/" + strings.TrimLeft(endpoint, "/")
	u.Path = basePath + cleanEndpoint
	return u.String()
}

// ComputeFallbackFingerprint produces a stable SHA-256 hex digest from sorted labels
// using canonical JSON encoding to eliminate delimiter and newline collision ambiguities.
func ComputeFallbackFingerprint(labels map[string]string) string {
	if labels == nil {
		labels = make(map[string]string)
	}
	canonicalJSON, err := json.Marshal(labels)
	if err != nil {
		keys := make([]string, 0, len(labels))
		for k := range labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		for _, k := range keys {
			v := labels[k]
			fmt.Fprintf(&buf, "%d:%s=%d:%s;", len(k), k, len(v), v)
		}
		canonicalJSON = buf.Bytes()
	}
	h := sha256.Sum256(canonicalJSON)
	return hex.EncodeToString(h[:])
}

// NormalizeTimestamp parses RFC3339 or RFC3339Nano and returns UTC RFC3339Nano,
// preserving fractional sub-second precision so that distinct occurrences within the
// same second retain distinct identities.
func NormalizeTimestamp(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty timestamp")
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return "", fmt.Errorf("invalid timestamp %q: %w", raw, err)
		}
	}
	return t.UTC().Format(time.RFC3339Nano), nil
}

func sanitizeError(err error, token string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if token != "" && strings.Contains(msg, token) {
		msg = strings.ReplaceAll(msg, token, "[REDACTED]")
	}
	return msg
}

// Sync performs a full alert snapshot synchronization:
// 1. Validates grafanaUrl and verifies grafanaToken is present.
// 2. Calls GET /api/org to verify source org ID == trustedOrgID.
// 3. Calls GET /api/alertmanager/grafana/api/v2/alerts with strict bounded JSON reading.
// 4. Applies snapshot to the store.
// If any failure occurs, active episodes are NEVER marked recovered, and the error is persisted.
func (s *Syncer) Sync(ctx context.Context, store *storage.Store, trustedOrgID int64, rawURL, token string) error {
	// Manual refresh and the poller must not apply overlapping snapshots out of order.
	s.mu.Lock()
	defer s.mu.Unlock()
	observedTime := time.Now().UTC()

	baseURL, err := ValidateSourceURL(rawURL)
	if err != nil {
		errMsg := sanitizeError(err, token)
		_ = store.SetSyncStatus(ctx, trustedOrgID, "", errMsg)
		return errors.New(errMsg)
	}

	token = strings.TrimSpace(token)
	if token == "" {
		err := errors.New("grafanaToken is required in secureJSONData")
		_ = store.SetSyncStatus(ctx, trustedOrgID, "", err.Error())
		return err
	}

	// 1. Verify GET /api/org preserving any configured subpath
	orgURL := joinURLPath(baseURL, "/api/org")
	orgReq, err := http.NewRequestWithContext(ctx, http.MethodGet, orgURL, nil)
	if err != nil {
		errMsg := sanitizeError(err, token)
		_ = store.SetSyncStatus(ctx, trustedOrgID, "", errMsg)
		return errors.New(errMsg)
	}
	orgReq.Header.Set("Authorization", "Bearer "+token)
	orgReq.Header.Set("Accept", "application/json")

	orgResp, err := s.client.Do(orgReq)
	if err != nil {
		errMsg := sanitizeError(fmt.Errorf("failed to query source /api/org: %w", err), token)
		_ = store.SetSyncStatus(ctx, trustedOrgID, "", errMsg)
		return errors.New(errMsg)
	}
	defer orgResp.Body.Close()

	if orgResp.StatusCode != http.StatusOK {
		err := fmt.Errorf("source /api/org returned HTTP status %d", orgResp.StatusCode)
		_ = store.SetSyncStatus(ctx, trustedOrgID, "", err.Error())
		return err
	}

	var orgData orgResponse
	if err := decodeCompleteJSON(orgResp.Body, 64*1024, &orgData); err != nil {
		err := fmt.Errorf("failed to decode source /api/org response: %w", err)
		_ = store.SetSyncStatus(ctx, trustedOrgID, "", err.Error())
		return err
	}

	if orgData.ID != trustedOrgID {
		err := fmt.Errorf("source org ID mismatch: source returned %d, trusted instance org is %d", orgData.ID, trustedOrgID)
		_ = store.SetSyncStatus(ctx, trustedOrgID, "", err.Error())
		return err
	}

	// 2. Query unfiltered alerts from GET /api/alertmanager/grafana/api/v2/alerts preserving subpath
	alertsURL := joinURLPath(baseURL, "/api/alertmanager/grafana/api/v2/alerts")
	alertsReq, err := http.NewRequestWithContext(ctx, http.MethodGet, alertsURL, nil)
	if err != nil {
		errMsg := sanitizeError(err, token)
		_ = store.SetSyncStatus(ctx, trustedOrgID, "", errMsg)
		return errors.New(errMsg)
	}
	alertsReq.Header.Set("Authorization", "Bearer "+token)
	alertsReq.Header.Set("Accept", "application/json")

	alertsResp, err := s.client.Do(alertsReq)
	if err != nil {
		errMsg := sanitizeError(fmt.Errorf("failed to query source alerts: %w", err), token)
		_ = store.SetSyncStatus(ctx, trustedOrgID, "", errMsg)
		return errors.New(errMsg)
	}
	defer alertsResp.Body.Close()

	if alertsResp.StatusCode != http.StatusOK {
		err := fmt.Errorf("source alerts endpoint returned HTTP status %d", alertsResp.StatusCode)
		_ = store.SetSyncStatus(ctx, trustedOrgID, "", err.Error())
		return err
	}

	var rawAlerts []alertmanagerAlert
	if err := decodeCompleteJSON(alertsResp.Body, maxAlertsResponseBytes, &rawAlerts); err != nil {
		err := fmt.Errorf("failed to decode complete alerts JSON (partial data rejected): %w", err)
		_ = store.SetSyncStatus(ctx, trustedOrgID, "", err.Error())
		return err
	}
	if rawAlerts == nil {
		err := errors.New("source alerts must be a non-null JSON array")
		_ = store.SetSyncStatus(ctx, trustedOrgID, "", err.Error())
		return err
	}

	// 3. Process active alerts
	var activeAlerts []storage.SnapshotAlert
	for _, a := range rawAlerts {
		state := strings.ToLower(strings.TrimSpace(a.Status.State))
		if state != "active" && state != "suppressed" && state != "unprocessed" {
			err := errors.New("source alert has an invalid or missing state")
			_ = store.SetSyncStatus(ctx, trustedOrgID, "", err.Error())
			return err
		}
		if strings.TrimSpace(a.Labels["alertname"]) == "" {
			err := errors.New("source alert has no alertname label")
			_ = store.SetSyncStatus(ctx, trustedOrgID, "", err.Error())
			return err
		}

		fp := strings.TrimSpace(a.Fingerprint)
		if fp == "" {
			fp = ComputeFallbackFingerprint(a.Labels)
		}

		normStarts, err := NormalizeTimestamp(a.StartsAt)
		if err != nil {
			err := errors.New("source alert has an invalid or missing start time")
			_ = store.SetSyncStatus(ctx, trustedOrgID, "", err.Error())
			return err
		}

		cleanLabels := sanitizer.NormalizeMap(a.Labels)
		cleanAnnotations := sanitizer.NormalizeMap(a.Annotations)
		cleanURL := sanitizer.SanitizeURL(a.GeneratorURL)

		activeAlerts = append(activeAlerts, storage.SnapshotAlert{
			Fingerprint:  fp,
			StartsAt:     normStarts,
			Labels:       cleanLabels,
			Annotations:  cleanAnnotations,
			GeneratorURL: cleanURL,
		})
	}

	// 4. Atomically sync snapshot to persistent store
	if err := store.SyncSnapshot(ctx, trustedOrgID, observedTime, activeAlerts); err != nil {
		errMsg := sanitizeError(fmt.Errorf("failed to sync snapshot to database: %w", err), token)
		_ = store.SetSyncStatus(ctx, trustedOrgID, "", errMsg)
		return errors.New(errMsg)
	}

	return nil
}
