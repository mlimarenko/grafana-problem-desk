package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

type configuredRule struct {
	GrafanaAlert *struct {
		UID     string `json:"uid"`
		Paused  *bool  `json:"is_paused"`
		Updated string `json:"updated"`
	} `json:"grafana_alert"`
}

type configuredGroup struct {
	Name  string           `json:"name"`
	Rules []configuredRule `json:"rules"`
}

type runtimeAlert struct {
	State    string            `json:"state"`
	Labels   map[string]string `json:"labels"`
	ActiveAt string            `json:"activeAt"`
}

type instanceState string

const (
	stateNormal     instanceState = "Normal"
	statePending    instanceState = "Pending"
	stateAlerting   instanceState = "Alerting"
	stateRecovering instanceState = "Recovering"
	stateNoData     instanceState = "NoData"
	stateError      instanceState = "Error"
)

// Grafana formats an instance as the base state followed by an optional reason.
func parseInstanceState(raw string) (instanceState, error) {
	base, suffix, hasReason := strings.Cut(raw, " (")
	if hasReason {
		if !strings.HasSuffix(suffix, ")") {
			return "", errors.New("invalid runtime alert state")
		}
		reason := suffix[:len(suffix)-1]
		if strings.TrimSpace(reason) == "" {
			return "", errors.New("invalid runtime alert state")
		}
		depth := 0
		for _, r := range reason {
			if unicode.IsControl(r) {
				return "", errors.New("invalid runtime alert state")
			}
			switch r {
			case '(':
				depth++
			case ')':
				depth--
				if depth < 0 {
					return "", errors.New("invalid runtime alert state")
				}
			}
		}
		if depth != 0 {
			return "", errors.New("invalid runtime alert state")
		}
	}
	state := instanceState(base)
	switch state {
	case stateNormal, statePending, stateAlerting, stateRecovering, stateNoData, stateError:
		return state, nil
	default:
		return "", errors.New("invalid runtime alert state")
	}
}

type runtimeRule struct {
	UID            string          `json:"uid"`
	State          string          `json:"state"`
	Paused         *bool           `json:"isPaused"`
	LastEvaluation string          `json:"lastEvaluation"`
	Alerts         json.RawMessage `json:"alerts"`
}

type runtimeRules struct {
	Status string `json:"status"`
	Data   *struct {
		Groups []struct {
			Rules []runtimeRule `json:"rules"`
		} `json:"groups"`
	} `json:"data"`
}

func (s *Syncer) readReadiness(ctx context.Context, base *url.URL, token, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, joinURLPath(base, path), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP %d", path, resp.StatusCode)
	}
	if err := decodeCompleteJSON(resp.Body, maxAlertsResponseBytes, out); err != nil {
		return fmt.Errorf("invalid %s response: %w", path, err)
	}
	return nil
}

// verifyReadiness refuses to infer recoveries from a notification snapshot until
// every configured unpaused rule has been evaluated and its firing instances
// appear in Alertmanager. Paused and deleted rules do not block recovery.
func (s *Syncer) verifyReadiness(ctx context.Context, base *url.URL, token string, alerts []alertmanagerAlert) error {
	var inventory map[string][]configuredGroup
	if err := s.readReadiness(ctx, base, token, "/api/ruler/grafana/api/v1/rules", &inventory); err != nil {
		return err
	}
	if inventory == nil {
		return errors.New("rule inventory must be a non-null object")
	}
	type configuration struct {
		paused  bool
		updated time.Time
	}
	configured := make(map[string]configuration)
	for namespace, groups := range inventory {
		if strings.TrimSpace(namespace) == "" || groups == nil {
			return errors.New("invalid rule inventory namespace")
		}
		for _, group := range groups {
			if strings.TrimSpace(group.Name) == "" || group.Rules == nil {
				return errors.New("invalid rule inventory group")
			}
			for _, rule := range group.Rules {
				if rule.GrafanaAlert == nil || strings.TrimSpace(rule.GrafanaAlert.UID) == "" || rule.GrafanaAlert.Paused == nil {
					return errors.New("invalid configured rule")
				}
				uid := rule.GrafanaAlert.UID
				updated, err := time.Parse(time.RFC3339Nano, rule.GrafanaAlert.Updated)
				if err != nil || updated.IsZero() {
					return errors.New("invalid configured rule update time")
				}
				if _, exists := configured[uid]; exists {
					return errors.New("duplicate configured rule")
				}
				configured[uid] = configuration{paused: *rule.GrafanaAlert.Paused, updated: updated}
			}
		}
	}
	var runtime runtimeRules
	if err := s.readReadiness(ctx, base, token, "/api/prometheus/grafana/api/v1/rules", &runtime); err != nil {
		return err
	}
	if runtime.Status != "success" || runtime.Data == nil || runtime.Data.Groups == nil {
		return errors.New("invalid runtime rule response")
	}
	seen := make(map[string]bool)
	deliveries := make(map[string]struct{}, len(alerts))
	for _, alert := range alerts {
		uid := alert.Labels["__alert_rule_uid__"]
		if uid == "" {
			continue
		}
		labels := make(map[string]string, len(alert.Labels)-1)
		for key, value := range alert.Labels {
			if key != "__alert_rule_uid__" {
				labels[key] = value
			}
		}
		start, err := NormalizeTimestamp(alert.StartsAt)
		if err != nil {
			return errors.New("invalid Alertmanager start time")
		}
		deliveries[deliveryKey(uid, start, labels)] = struct{}{}
	}
	for _, group := range runtime.Data.Groups {
		if group.Rules == nil {
			return errors.New("invalid runtime rule group")
		}
		for _, rule := range group.Rules {
			if strings.TrimSpace(rule.UID) == "" || rule.Paused == nil {
				return errors.New("invalid runtime rule")
			}
			config, exists := configured[rule.UID]
			if !exists || config.paused {
				continue
			}
			if seen[rule.UID] || *rule.Paused {
				return errors.New("inconsistent runtime rule")
			}
			seen[rule.UID] = true
			evaluation, err := time.Parse(time.RFC3339Nano, rule.LastEvaluation)
			if err != nil || evaluation.IsZero() || evaluation.Before(config.updated) {
				return errors.New("configured rule has not been evaluated")
			}
			switch rule.State {
			case "inactive", "pending", "firing":
			default:
				return errors.New("invalid runtime rule state")
			}
			if len(rule.Alerts) == 0 {
				return errors.New("runtime rule is missing alert instances")
			}
			var instances []runtimeAlert
			if err := json.Unmarshal(rule.Alerts, &instances); err != nil {
				return errors.New("invalid runtime alert instances")
			}
			if rule.State == "firing" && len(instances) == 0 {
				return errors.New("firing rule has no runtime instances")
			}
			hasFiring, hasPending := false, false
			for _, instance := range instances {
				if strings.TrimSpace(instance.Labels["alertname"]) == "" {
					return errors.New("runtime alert is missing labels")
				}
				labels := make(map[string]string, len(instance.Labels)+1)
				for key, value := range instance.Labels {
					if key != "" && value != "" {
						labels[key] = value
					}
				}
				state, err := parseInstanceState(instance.State)
				if err != nil {
					return err
				}
				switch state {
				case statePending:
					hasPending = true
					continue
				case stateNormal:
					continue
				case stateNoData, stateError:
					labels["rulename"] = labels["alertname"]
					if state == stateNoData {
						labels["alertname"] = "DatasourceNoData"
					} else {
						labels["alertname"] = "DatasourceError"
					}
				case stateAlerting, stateRecovering:
					hasFiring = true
				}
				start, err := NormalizeTimestamp(instance.ActiveAt)
				_, delivered := deliveries[deliveryKey(rule.UID, start, labels)]
				if err != nil || !delivered {
					return errors.New("firing runtime alert is not present in Alertmanager")
				}
			}
			if rule.State == "firing" && !hasFiring || rule.State == "pending" && !hasPending {
				return errors.New("runtime rule state is inconsistent with its instances")
			}
		}
	}
	for uid, config := range configured {
		if !config.paused && !seen[uid] {
			return errors.New("configured rule is not loaded in the evaluator")
		}
	}
	return nil
}

func deliveryKey(uid, start string, labels map[string]string) string {
	// JSON encoding of strings and string maps cannot fail and sorts map keys.
	key, _ := json.Marshal(struct {
		UID, Start string
		Labels     map[string]string
	}{uid, start, labels})
	return string(key)
}
