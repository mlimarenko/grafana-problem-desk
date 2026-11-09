package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
)

func (a *App) handleResolve(ctx context.Context, orgID int64, q url.Values, sender backend.CallResourceResponseSender) error {
	bad := func() error {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid query"})
	}
	for key, v := range q {
		if (key != "fingerprint" && key != "labels" && key != "startsAt" && key != "matchers") || len(v) != 1 {
			return bad()
		}
	}
	fp, hasFP := q["fingerprint"]
	raw, hasLabels := q["labels"]
	matchersRaw, hasMatchers := q["matchers"]
	if hasMatchers {
		if hasFP || hasLabels || len(q["startsAt"]) != 0 {
			return bad()
		}
		matchers, err := parseResolverLabels(matchersRaw[0])
		if err != nil || matchers["__alert_rule_uid__"] == "" || matchers["instance"] == "" {
			return bad()
		}
		id, err := a.store.ResolveActiveProblem(ctx, orgID, matchers)
		return sendResolveResult(sender, id, err)
	}
	if hasFP == hasLabels || len(q["startsAt"]) != 1 {
		return bad()
	}
	starts, err := parseProblemInstant(q.Get("startsAt"))
	if err != nil {
		return bad()
	}
	var fingerprint string
	var labels map[string]string
	if hasFP {
		fingerprint = fp[0]
		if fingerprint == "" || len(fingerprint) > 1024 {
			return bad()
		}
	} else {
		labels, err = parseResolverLabels(raw[0])
		if err != nil {
			return bad()
		}
	}
	id, err := a.store.ResolveProblem(ctx, orgID, fingerprint, labels, starts)
	return sendResolveResult(sender, id, err)
}

func parseResolverLabels(raw string) (map[string]string, error) {
	bad := errors.New("invalid labels")
	labels := map[string]string{}
	dec := json.NewDecoder(strings.NewReader(raw))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, bad
	}
	for dec.More() {
		token, err = dec.Token()
		if err != nil {
			return nil, bad
		}
		key, ok := token.(string)
		if !ok || key == "" || len(key) > 1024 || len(labels) >= 64 {
			return nil, bad
		}
		if _, exists := labels[key]; exists {
			return nil, bad
		}
		var value *string
		if err = dec.Decode(&value); err != nil || value == nil || len(*value) > 4096 {
			return nil, bad
		}
		labels[key] = *value
	}
	if token, err = dec.Token(); err != nil || token != json.Delim('}') || len(labels) == 0 {
		return nil, bad
	}
	if _, err = dec.Token(); err != io.EOF {
		return nil, bad
	}
	return labels, nil
}

func sendResolveResult(sender backend.CallResourceResponseSender, id string, err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return sendJSON(sender, http.StatusNotFound, map[string]string{"error": "problem not found"})
	}
	if errors.Is(err, storage.ErrAmbiguous) {
		return sendJSON(sender, http.StatusConflict, map[string]string{"error": "ambiguous problem"})
	}
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to resolve problem"})
	}
	return sendJSON(sender, http.StatusOK, map[string]string{"problemId": id})
}
