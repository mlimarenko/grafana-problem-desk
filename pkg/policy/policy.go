// Package policy owns the Problem Desk organization policy document: strict
// decoding, validation, compilation and the derived classification/workflow
// lookups used by storage and the HTTP API.
package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Schema and document bounds from the 0.3.0 contract.
const (
	SchemaVersion                = 1
	MaxDocumentBytes             = 48 * 1024
	DefaultRecoveredTrustMinutes = 60
	MaxLevels                    = 16
	MaxWorkflows                 = 8
	MaxStatesPerWorkflow         = 12
	MaxTransitionsPerWorkflow    = 64
	MaxRoutingRules              = 16
	MaxRouteLabels               = 8
	MaxAliasesPerLevel           = 32
	MaxAliasRunes                = 64
	MaxNameRunes                 = 80
	MaxLevelRank                 = 100
	MinRecoveredTrustMinutes     = 5
	MaxRecoveredTrustMinutes     = 1440
)

var (
	idPattern          = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
	severityKeyPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$`)
)

// Reserved identifiers must never become dictionary keys in a JavaScript client.
var reservedIDs = map[string]struct{}{
	"__proto__":      {},
	"prototype":      {},
	"constructor":    {},
	"tostring":       {},
	"valueof":        {},
	"hasownproperty": {},
}

var validTones = map[string]struct{}{
	"red": {}, "orange": {}, "yellow": {}, "blue": {}, "green": {}, "purple": {}, "grey": {},
}

var validWindows = map[string]struct{}{
	"1h": {}, "24h": {}, "7d": {}, "30d": {},
}

var validColumns = map[string]struct{}{
	"level": {}, "name": {}, "status": {}, "started": {}, "duration": {}, "owner": {}, "ack": {},
}

// Minimum native Grafana roles for workflow actions. A policy can never grant
// Viewer mutation rights; these values only raise the required editor baseline.
const (
	MinimumRoleEditor = "editor"
	MinimumRoleAdmin  = "admin"
)

// EffectiveMinimumRole applies the editor default to an omitted role field.
func EffectiveMinimumRole(role string) string {
	if role == "" {
		return MinimumRoleEditor
	}
	return role
}

// ValidMinimumRole reports whether role is a supported workflow action role.
func ValidMinimumRole(role string) bool {
	return role == "" || role == MinimumRoleEditor || role == MinimumRoleAdmin
}

// RoleAtLeast reports whether the trusted actor role satisfies the configured
// minimum. Viewer is never sufficient for a workflow action.
func RoleAtLeast(actorRole, required string) bool {
	rank := func(role string) int {
		switch role {
		case "admin":
			return 3
		case "editor":
			return 2
		case "viewer":
			return 1
		default:
			return 0
		}
	}
	return rank(actorRole) >= rank(EffectiveMinimumRole(required)) && rank(actorRole) >= 2
}

// LocalizedText is a required Russian/English label.
type LocalizedText struct {
	RU string `json:"ru"`
	EN string `json:"en"`
}

// Level is a configured importance level.
type Level struct {
	ID      string        `json:"id"`
	Rank    int           `json:"rank"`
	Tone    string        `json:"tone"`
	Name    LocalizedText `json:"name"`
	Aliases []string      `json:"aliases"`
}

// State is a state of an operator workflow.
type State struct {
	ID               string        `json:"id"`
	Rank             int           `json:"rank"`
	Name             LocalizedText `json:"name"`
	OperatorComplete bool          `json:"operatorComplete,omitempty"`
}

// Requires lists the gates that must hold before a workflow action is allowed.
type Requires struct {
	Ack             bool `json:"ack,omitempty"`
	Owner           bool `json:"owner,omitempty"`
	Comment         bool `json:"comment,omitempty"`
	SourceRecovered bool `json:"sourceRecovered,omitempty"`
}

// Any reports whether at least one gate is required.
func (r Requires) Any() bool {
	return r.Ack || r.Owner || r.Comment || r.SourceRecovered
}

// Transition is an explicit directed edge between two workflow states.
type Transition struct {
	From        string   `json:"from"`
	To          string   `json:"to"`
	MinimumRole string   `json:"minimumRole"`
	Requires    Requires `json:"requires"`
}

// Workflow is an operator workflow definition.
type Workflow struct {
	ID            string        `json:"id"`
	Name          LocalizedText `json:"name"`
	InitialState  string        `json:"initialState"`
	MinimumRole   string        `json:"minimumRole"`
	StartRequires Requires      `json:"startRequires"`
	States        []State       `json:"states"`
	Transitions   []Transition  `json:"transitions"`
}

// RouteMatch selects a workflow by exact source label values and/or a derived level.
type RouteMatch struct {
	Labels map[string]string `json:"labels"`
	Level  string            `json:"level,omitempty"`
}

// Route is one ordered routing rule.
type Route struct {
	Match    RouteMatch `json:"match"`
	Workflow string     `json:"workflow"`
}

// Views holds display defaults only.
type Views struct {
	Recent  string   `json:"recent"`
	History string   `json:"history"`
	Columns []string `json:"columns"`
}

// Source holds source-trust settings.
type Source struct {
	RecoveredTrustMinutes *int `json:"recoveredTrustMinutes,omitempty"`
}

// Document is the schemaVersion 1 organization policy document.
type Document struct {
	SchemaVersion   int        `json:"schemaVersion"`
	SeverityKey     string     `json:"severityKey"`
	Levels          []Level    `json:"levels"`
	Workflows       []Workflow `json:"workflows"`
	Routing         []Route    `json:"routing"`
	DefaultWorkflow *string    `json:"defaultWorkflow"`
	Views           Views      `json:"views"`
	Source          Source     `json:"source"`
}

// Issue is a stable validation finding addressed by a JSONPath-like location.
type Issue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidationError carries validation or occupancy findings from a rejected document.
type ValidationError struct {
	Issues []Issue
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("policy document is invalid (%d issue(s))", len(e.Issues))
}

// TrustMinutes returns the configured source-recovery trust window.
func (d *Document) TrustMinutes() int {
	if d.Source.RecoveredTrustMinutes == nil {
		return DefaultRecoveredTrustMinutes
	}
	return *d.Source.RecoveredTrustMinutes
}

// StringPtr is a helper for optional document fields.
func StringPtr(value string) *string {
	return &value
}

// IntPtr is a helper for optional document fields.
func IntPtr(value int) *int {
	return &value
}

// DefaultDoc returns the built-in policy that reproduces the 0.2 classification.
func DefaultDoc() *Document {
	return &Document{
		SchemaVersion: SchemaVersion,
		SeverityKey:   "severity",
		Levels: []Level{
			{ID: "disaster", Rank: 6, Tone: "red", Name: LocalizedText{RU: "Катастрофа", EN: "Disaster"}, Aliases: []string{"disaster"}},
			{ID: "critical", Rank: 5, Tone: "red", Name: LocalizedText{RU: "Критический", EN: "Critical"}, Aliases: []string{"critical"}},
			{ID: "high", Rank: 4, Tone: "orange", Name: LocalizedText{RU: "Высокий", EN: "High"}, Aliases: []string{"high"}},
			{ID: "warning", Rank: 3, Tone: "yellow", Name: LocalizedText{RU: "Предупреждение", EN: "Warning"}, Aliases: []string{"warning", "average"}},
			{ID: "info", Rank: 2, Tone: "blue", Name: LocalizedText{RU: "Информация", EN: "Info"}, Aliases: []string{"info", "information"}},
			{ID: "unknown", Rank: 1, Tone: "grey", Name: LocalizedText{RU: "Неизвестно", EN: "Unknown"}, Aliases: []string{}},
		},
		Workflows: []Workflow{
			{
				ID:            "basic",
				Name:          LocalizedText{RU: "Базовый", EN: "Basic"},
				InitialState:  "new",
				MinimumRole:   MinimumRoleEditor,
				StartRequires: Requires{},
				States: []State{
					{ID: "new", Rank: 10, Name: LocalizedText{RU: "Новый", EN: "New"}},
					{ID: "in_progress", Rank: 20, Name: LocalizedText{RU: "В работе", EN: "In progress"}},
					{ID: "resolved", Rank: 30, Name: LocalizedText{RU: "Решён", EN: "Resolved"}, OperatorComplete: true},
				},
				Transitions: []Transition{
					{From: "new", To: "in_progress", MinimumRole: MinimumRoleEditor, Requires: Requires{}},
					{From: "in_progress", To: "resolved", MinimumRole: MinimumRoleEditor, Requires: Requires{Ack: true, SourceRecovered: true}},
					{From: "resolved", To: "in_progress", MinimumRole: MinimumRoleEditor, Requires: Requires{Comment: true}},
				},
			},
		},
		Routing:         []Route{{Match: RouteMatch{Labels: map[string]string{"team": "payments"}}, Workflow: "basic"}},
		DefaultWorkflow: nil,
		Views: Views{
			Recent:  "24h",
			History: "7d",
			Columns: []string{"level", "name", "status", "started", "duration", "owner", "ack"},
		},
		Source: Source{RecoveredTrustMinutes: IntPtr(DefaultRecoveredTrustMinutes)},
	}
}

// CanonicalJSON encodes a document deterministically: struct field order is
// fixed and encoding/json sorts map keys. HTML escaping is disabled so the
// stored bytes stay readable and stable across revisions.
func CanonicalJSON(doc *Document) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(doc); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Hash returns the cache identity of canonical document bytes.
func Hash(canonical []byte) string {
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// ParseDocument strictly decodes and validates a policy document.
func ParseDocument(data []byte) (*Document, []Issue) {
	if len(data) > MaxDocumentBytes {
		return nil, []Issue{{Path: "$", Code: "too_long", Message: fmt.Sprintf("policy document exceeds %d bytes", MaxDocumentBytes)}}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, []Issue{{Path: "$", Code: "required", Message: "policy document is required"}}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var doc Document
	if err := decoder.Decode(&doc); err != nil {
		return nil, []Issue{{Path: "$", Code: "invalid", Message: err.Error()}}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, []Issue{{Path: "$", Code: "invalid", Message: "unexpected trailing JSON data"}}
	}
	Normalize(&doc)
	if issues := Validate(&doc); len(issues) > 0 {
		return &doc, issues
	}
	return &doc, nil
}

// Normalize materializes optional collections and defaults so the stored
// canonical document always round-trips as arrays, objects and an explicit
// source trust window for every client.
func Normalize(doc *Document) {
	if doc.Source.RecoveredTrustMinutes == nil {
		doc.Source.RecoveredTrustMinutes = IntPtr(DefaultRecoveredTrustMinutes)
	}
	if doc.Workflows == nil {
		doc.Workflows = []Workflow{}
	}
	if doc.Routing == nil {
		doc.Routing = []Route{}
	}
	for i := range doc.Levels {
		if doc.Levels[i].Aliases == nil {
			doc.Levels[i].Aliases = []string{}
		}
	}
	for i := range doc.Workflows {
		doc.Workflows[i].MinimumRole = EffectiveMinimumRole(doc.Workflows[i].MinimumRole)
		if doc.Workflows[i].States == nil {
			doc.Workflows[i].States = []State{}
		}
		if doc.Workflows[i].Transitions == nil {
			doc.Workflows[i].Transitions = []Transition{}
		}
		for j := range doc.Workflows[i].Transitions {
			doc.Workflows[i].Transitions[j].MinimumRole = EffectiveMinimumRole(doc.Workflows[i].Transitions[j].MinimumRole)
		}
	}
	for i := range doc.Routing {
		if doc.Routing[i].Match.Labels == nil {
			doc.Routing[i].Match.Labels = map[string]string{}
		}
	}
}

// Validate returns all schema and invariant issues in document order.
func Validate(doc *Document) []Issue {
	issues := make([]Issue, 0)
	add := func(path, code, message string) {
		issues = append(issues, Issue{Path: path, Code: code, Message: message})
	}

	if doc.SchemaVersion != SchemaVersion {
		code := "invalid"
		if doc.SchemaVersion == 0 {
			code = "required"
		}
		add("$.schemaVersion", code, fmt.Sprintf("schemaVersion must be %d", SchemaVersion))
	}
	if doc.SeverityKey == "" {
		add("$.severityKey", "required", "severityKey is required")
	} else if !severityKeyPattern.MatchString(doc.SeverityKey) {
		add("$.severityKey", "invalid", "severityKey must match ^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$")
	}

	validateLevels(doc, add)
	validateWorkflows(doc, add)
	validateRouting(doc, add)

	if doc.DefaultWorkflow != nil {
		value := *doc.DefaultWorkflow
		if value == "" {
			add("$.defaultWorkflow", "invalid", "defaultWorkflow must be a workflow id or null")
		} else if !hasWorkflow(doc, value) {
			add("$.defaultWorkflow", "unknown_ref", fmt.Sprintf("unknown workflow %q", value))
		}
	}

	if _, ok := validWindows[doc.Views.Recent]; !ok {
		code := "invalid"
		if doc.Views.Recent == "" {
			code = "required"
		}
		add("$.views.recent", code, "recent must be one of 1h, 24h, 7d, 30d")
	}
	if _, ok := validWindows[doc.Views.History]; !ok {
		code := "invalid"
		if doc.Views.History == "" {
			code = "required"
		}
		add("$.views.history", code, "history must be one of 1h, 24h, 7d, 30d")
	}
	if len(doc.Views.Columns) == 0 {
		add("$.views.columns", "required", "columns must not be empty")
	}
	seenColumn := make(map[string]struct{}, len(doc.Views.Columns))
	for i, column := range doc.Views.Columns {
		path := fmt.Sprintf("$.views.columns[%d]", i)
		if _, allowed := validColumns[column]; !allowed {
			add(path, "invalid", fmt.Sprintf("unknown column %q", column))
			continue
		}
		if _, exists := seenColumn[column]; exists {
			add(path, "duplicate", fmt.Sprintf("duplicate column %q", column))
		}
		seenColumn[column] = struct{}{}
	}

	if minutes := doc.Source.RecoveredTrustMinutes; minutes != nil {
		if *minutes < MinRecoveredTrustMinutes || *minutes > MaxRecoveredTrustMinutes {
			add("$.source.recoveredTrustMinutes", "invalid", fmt.Sprintf("recoveredTrustMinutes must be between %d and %d", MinRecoveredTrustMinutes, MaxRecoveredTrustMinutes))
		}
	}

	return issues
}

func validateLevels(doc *Document, add func(path, code, message string)) {
	if len(doc.Levels) == 0 {
		add("$.levels", "required", "at least one level is required")
	}
	if len(doc.Levels) > MaxLevels {
		add("$.levels", "too_many", fmt.Sprintf("at most %d levels are allowed", MaxLevels))
	}
	seenIDs := make(map[string]struct{}, len(doc.Levels))
	seenAliases := make(map[string]string, len(doc.Levels)*4)
	for i, level := range doc.Levels {
		path := fmt.Sprintf("$.levels[%d]", i)
		validateID(path+".id", level.ID, seenIDs, add)
		if level.Rank < 0 || level.Rank > MaxLevelRank {
			add(path+".rank", "invalid", fmt.Sprintf("rank must be between 0 and %d", MaxLevelRank))
		}
		if _, ok := validTones[level.Tone]; !ok {
			add(path+".tone", "invalid", fmt.Sprintf("tone must be one of red, orange, yellow, blue, green, purple, grey"))
		}
		validateName(path+".name", level.Name, add)
		if len(level.Aliases) > MaxAliasesPerLevel {
			add(path+".aliases", "too_many", fmt.Sprintf("at most %d aliases are allowed", MaxAliasesPerLevel))
		}
		for j, alias := range level.Aliases {
			aliasPath := fmt.Sprintf("%s.aliases[%d]", path, j)
			runes := utf8.RuneCountInString(alias)
			if runes == 0 {
				add(aliasPath, "required", "alias must not be empty")
				continue
			}
			if runes > MaxAliasRunes {
				add(aliasPath, "too_long", fmt.Sprintf("alias must be at most %d characters", MaxAliasRunes))
				continue
			}
			if hasControl(alias) {
				add(aliasPath, "invalid", "alias must not contain control characters")
				continue
			}
			lowered := lower(alias)
			if owner, exists := seenAliases[lowered]; exists {
				add(aliasPath, "duplicate", fmt.Sprintf("alias %q is already used by level %q", alias, owner))
				continue
			}
			seenAliases[lowered] = level.ID
		}
	}
}

func validateWorkflows(doc *Document, add func(path, code, message string)) {
	if len(doc.Workflows) > MaxWorkflows {
		add("$.workflows", "too_many", fmt.Sprintf("at most %d workflows are allowed", MaxWorkflows))
	}
	seenWorkflows := make(map[string]struct{}, len(doc.Workflows))
	for i, workflow := range doc.Workflows {
		path := fmt.Sprintf("$.workflows[%d]", i)
		validateID(path+".id", workflow.ID, seenWorkflows, add)
		validateName(path+".name", workflow.Name, add)
		if !ValidMinimumRole(workflow.MinimumRole) {
			add(path+".minimumRole", "invalid", "minimumRole must be editor or admin")
		}

		if len(workflow.States) == 0 {
			add(path+".states", "required", "at least one state is required")
		}
		if len(workflow.States) > MaxStatesPerWorkflow {
			add(path+".states", "too_many", fmt.Sprintf("at most %d states are allowed", MaxStatesPerWorkflow))
		}
		states := make(map[string]struct{}, len(workflow.States))
		ranks := make(map[int]string, len(workflow.States))
		completeStates := make([]string, 0, 1)
		for j, state := range workflow.States {
			statePath := fmt.Sprintf("%s.states[%d]", path, j)
			if state.ID != "" {
				if _, exists := states[state.ID]; exists {
					add(statePath+".id", "duplicate", fmt.Sprintf("duplicate state id %q", state.ID))
				}
			}
			validateID(statePath+".id", state.ID, nil, add)
			validateName(statePath+".name", state.Name, add)
			if state.Rank < 0 {
				add(statePath+".rank", "invalid", "rank must not be negative")
			}
			if owner, exists := ranks[state.Rank]; exists {
				add(statePath+".rank", "duplicate", fmt.Sprintf("state rank %d is already used by %q", state.Rank, owner))
			} else {
				ranks[state.Rank] = state.ID
			}
			if state.OperatorComplete {
				completeStates = append(completeStates, state.ID)
			}
			states[state.ID] = struct{}{}
		}
		if len(completeStates) > 1 {
			add(path+".states", "too_many", "at most one state may be operatorComplete")
		}

		if workflow.InitialState == "" {
			add(path+".initialState", "required", "initialState is required")
		} else if _, ok := states[workflow.InitialState]; !ok {
			add(path+".initialState", "unknown_ref", fmt.Sprintf("initialState %q is not a state of this workflow", workflow.InitialState))
		}

		if len(workflow.Transitions) > MaxTransitionsPerWorkflow {
			add(path+".transitions", "too_many", fmt.Sprintf("at most %d transitions are allowed", MaxTransitionsPerWorkflow))
		}
		edges := make(map[string]struct{}, len(workflow.Transitions))
		adjacent := make(map[string][]string, len(workflow.States))
		for j, transition := range workflow.Transitions {
			transitionPath := fmt.Sprintf("%s.transitions[%d]", path, j)
			if !ValidMinimumRole(transition.MinimumRole) {
				add(transitionPath+".minimumRole", "invalid", "minimumRole must be editor or admin")
			}
			if transition.From == "" {
				add(transitionPath+".from", "required", "from is required")
			} else if _, ok := states[transition.From]; !ok {
				add(transitionPath+".from", "unknown_ref", fmt.Sprintf("unknown state %q", transition.From))
			}
			if transition.To == "" {
				add(transitionPath+".to", "required", "to is required")
			} else if _, ok := states[transition.To]; !ok {
				add(transitionPath+".to", "unknown_ref", fmt.Sprintf("unknown state %q", transition.To))
			}
			if transition.From != "" && transition.From == transition.To {
				add(transitionPath, "self_loop", "a transition must not target its own state")
				continue
			}
			key := transition.From + "\x00" + transition.To
			if _, exists := edges[key]; exists {
				add(transitionPath, "duplicate", fmt.Sprintf("duplicate transition %q -> %q", transition.From, transition.To))
				continue
			}
			edges[key] = struct{}{}
			adjacent[transition.From] = append(adjacent[transition.From], transition.To)
		}

		if workflow.InitialState != "" {
			if _, ok := states[workflow.InitialState]; ok {
				reachable := map[string]bool{workflow.InitialState: true}
				queue := []string{workflow.InitialState}
				for len(queue) > 0 {
					current := queue[0]
					queue = queue[1:]
					for _, next := range adjacent[current] {
						if !reachable[next] {
							reachable[next] = true
							queue = append(queue, next)
						}
					}
				}
				for j, state := range workflow.States {
					if !reachable[state.ID] {
						add(fmt.Sprintf("%s.states[%d]", path, j), "unreachable", fmt.Sprintf("state %q is unreachable from initialState %q", state.ID, workflow.InitialState))
					}
				}
			}
		}
	}
}

func validateRouting(doc *Document, add func(path, code, message string)) {
	if len(doc.Routing) > MaxRoutingRules {
		add("$.routing", "too_many", fmt.Sprintf("at most %d routing rules are allowed", MaxRoutingRules))
	}
	levels := make(map[string]struct{}, len(doc.Levels))
	for _, level := range doc.Levels {
		levels[level.ID] = struct{}{}
	}
	for i, route := range doc.Routing {
		path := fmt.Sprintf("$.routing[%d]", i)
		if route.Workflow == "" {
			add(path+".workflow", "required", "workflow is required")
		} else if !hasWorkflow(doc, route.Workflow) {
			add(path+".workflow", "unknown_ref", fmt.Sprintf("unknown workflow %q", route.Workflow))
		}
		if len(route.Match.Labels) > MaxRouteLabels {
			add(path+".match.labels", "too_many", fmt.Sprintf("at most %d label selectors are allowed", MaxRouteLabels))
		}
		for key := range route.Match.Labels {
			if key == "" {
				add(path+".match.labels", "required", "label selector key must not be empty")
			}
			// Empty selector values are literal and valid.
		}
		if level := route.Match.Level; level != "" {
			if _, ok := levels[level]; !ok {
				add(path+".match.level", "unknown_ref", fmt.Sprintf("unknown level %q", level))
			}
		}
	}
}

func validateID(path, value string, seen map[string]struct{}, add func(path, code, message string)) {
	if value == "" {
		add(path, "required", "id is required")
		return
	}
	if !idPattern.MatchString(value) {
		add(path, "invalid", "id must match ^[a-z0-9_]{1,32}$")
		return
	}
	if _, reserved := reservedIDs[value]; reserved {
		add(path, "reserved_id", fmt.Sprintf("id %q is reserved", value))
	}
	if seen != nil {
		if _, exists := seen[value]; exists {
			add(path, "duplicate", fmt.Sprintf("duplicate id %q", value))
			return
		}
		seen[value] = struct{}{}
	}
}

func validateName(path string, name LocalizedText, add func(path, code, message string)) {
	validateText(path+".ru", name.RU, add)
	validateText(path+".en", name.EN, add)
}

func validateText(path, value string, add func(path, code, message string)) {
	runes := utf8.RuneCountInString(value)
	if runes == 0 {
		add(path, "required", "text is required")
		return
	}
	if runes > MaxNameRunes {
		add(path, "too_long", fmt.Sprintf("text must be at most %d characters", MaxNameRunes))
		return
	}
	if hasControl(value) {
		add(path, "invalid", "text must not contain control characters")
	}
}

func hasControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func lower(value string) string {
	return strings.ToLower(value)
}

func hasWorkflow(doc *Document, id string) bool {
	for _, workflow := range doc.Workflows {
		if workflow.ID == id {
			return true
		}
	}
	return false
}
