package policy_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mlimarenko/grafana-problem-desk/pkg/policy"
)

func mustParse(t *testing.T, data []byte) *policy.Document {
	t.Helper()
	doc, issues := policy.ParseDocument(data)
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %+v", issues)
	}
	return doc
}

func hasIssue(issues []policy.Issue, path, code string) bool {
	for _, issue := range issues {
		if issue.Path == path && issue.Code == code {
			return true
		}
	}
	return false
}

func canonicalDefault(t *testing.T) []byte {
	t.Helper()
	data, err := policy.CanonicalJSON(policy.DefaultDoc())
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDefaultDocumentRanksAndResolution(t *testing.T) {
	doc := policy.DefaultDoc()
	if issues := policy.Validate(doc); len(issues) != 0 {
		t.Fatalf("default document is invalid: %+v", issues)
	}
	ranks := map[string]int{"disaster": 6, "critical": 5, "high": 4, "warning": 3, "info": 2, "unknown": 1}
	for _, level := range doc.Levels {
		if ranks[level.ID] != level.Rank {
			t.Errorf("level %s rank %d, want %d", level.ID, level.Rank, ranks[level.ID])
		}
	}
	compiled, err := policy.Compile(doc, 1)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		raw     string
		levelID string
		matched bool
	}{
		{"disaster", "disaster", true},
		{"CRITICAL", "critical", true},
		{"average", "warning", true},
		{"information", "info", true},
		{"unknown", "unknown", false},
		{" critical ", "unknown", false},
		{"", "unknown", false},
		{"nonsense", "unknown", false},
	}
	for _, tc := range cases {
		match := compiled.ResolveLevel(tc.raw)
		if match.Level.ID != tc.levelID || match.Matched != tc.matched {
			t.Errorf("resolve %q = (%s, %v), want (%s, %v)", tc.raw, match.Level.ID, match.Matched, tc.levelID, tc.matched)
		}
	}
}

func TestFallbackWithoutUnknownUsesLowestRankAndIDOrder(t *testing.T) {
	doc := policy.DefaultDoc()
	name := policy.LocalizedText{RU: "Имя", EN: "Name"}
	doc.Levels = []policy.Level{
		{ID: "beta", Rank: 3, Tone: "blue", Name: name, Aliases: []string{"beta"}},
		{ID: "alpha", Rank: 3, Tone: "blue", Name: name, Aliases: []string{"alpha"}},
	}
	doc.Routing = nil
	compiled, err := policy.Compile(doc, 7)
	if err != nil {
		t.Fatalf("compile: %+v", err)
	}
	if match := compiled.ResolveLevel("zzz"); match.Level.ID != "alpha" || match.Matched {
		t.Fatalf("fallback = (%s, %v), want (alpha, false)", match.Level.ID, match.Matched)
	}
}

func TestValidationMatrix(t *testing.T) {
	tooManyLevels := func(doc *policy.Document) {
		base := doc.Levels[0]
		for len(doc.Levels) < policy.MaxLevels+1 {
			clone := base
			clone.ID = "level_" + string(rune('a'+len(doc.Levels)))
			clone.Aliases = []string{clone.ID}
			doc.Levels = append(doc.Levels, clone)
		}
	}
	tooManyAliases := func(doc *policy.Document) {
		aliases := make([]string, 0, policy.MaxAliasesPerLevel+1)
		for i := 0; i <= policy.MaxAliasesPerLevel; i++ {
			aliases = append(aliases, "alias_"+string(rune('a'+i)))
		}
		doc.Levels[0].Aliases = aliases
	}
	nineRouteLabels := func(doc *policy.Document) {
		labels := make(map[string]string, 9)
		for i := 0; i < 9; i++ {
			labels["key_"+string(rune('a'+i))] = "value"
		}
		doc.Routing[0].Match.Labels = labels
	}

	cases := []struct {
		name   string
		path   string
		code   string
		mutate func(*policy.Document)
	}{
		{"schema version", "$.schemaVersion", "invalid", func(d *policy.Document) { d.SchemaVersion = 2 }},
		{"missing schema version", "$.schemaVersion", "required", func(d *policy.Document) { d.SchemaVersion = 0 }},
		{"severity key required", "$.severityKey", "required", func(d *policy.Document) { d.SeverityKey = "" }},
		{"severity key pattern", "$.severityKey", "invalid", func(d *policy.Document) { d.SeverityKey = "1bad" }},
		{"no levels", "$.levels", "required", func(d *policy.Document) { d.Levels = nil }},
		{"too many levels", "$.levels", "too_many", tooManyLevels},
		{"reserved level id", "$.levels[0].id", "reserved_id", func(d *policy.Document) { d.Levels[0].ID = "__proto__" }},
		{"invalid level id", "$.levels[0].id", "invalid", func(d *policy.Document) { d.Levels[0].ID = "Bad" }},
		{"duplicate level id", "$.levels[1].id", "duplicate", func(d *policy.Document) { d.Levels[1].ID = "disaster" }},
		{"level rank", "$.levels[0].rank", "invalid", func(d *policy.Document) { d.Levels[0].Rank = -1 }},
		{"tone", "$.levels[0].tone", "invalid", func(d *policy.Document) { d.Levels[0].Tone = "pink" }},
		{"name required", "$.levels[0].name.en", "required", func(d *policy.Document) { d.Levels[0].Name.EN = "" }},
		{"name too long", "$.levels[0].name.ru", "too_long", func(d *policy.Document) {
			d.Levels[0].Name.RU = strings.Repeat("я", policy.MaxNameRunes+1)
		}},
		{"name control", "$.levels[0].name.en", "invalid", func(d *policy.Document) { d.Levels[0].Name.EN = "bad\x00name" }},
		{"empty alias", "$.levels[0].aliases[0]", "required", func(d *policy.Document) { d.Levels[0].Aliases = []string{""} }},
		{"long alias", "$.levels[0].aliases[0]", "too_long", func(d *policy.Document) {
			d.Levels[0].Aliases = []string{strings.Repeat("x", policy.MaxAliasRunes+1)}
		}},
		{"control alias", "$.levels[0].aliases[0]", "invalid", func(d *policy.Document) {
			d.Levels[0].Aliases = []string{"bad\nline"}
		}},
		{"alias collision", "$.levels[5].aliases[0]", "duplicate", func(d *policy.Document) {
			d.Levels[5].Aliases = []string{"Critical"}
		}},
		{"too many aliases", "$.levels[0].aliases", "too_many", tooManyAliases},
		{"reserved workflow id", "$.workflows[0].id", "reserved_id", func(d *policy.Document) { d.Workflows[0].ID = "constructor" }},
		{"unknown initial state", "$.workflows[0].initialState", "unknown_ref", func(d *policy.Document) {
			d.Workflows[0].InitialState = "ghost"
		}},
		{"duplicate state id", "$.workflows[0].states[1].id", "duplicate", func(d *policy.Document) {
			d.Workflows[0].States[1].ID = "new"
		}},
		{"duplicate state rank", "$.workflows[0].states[1].rank", "duplicate", func(d *policy.Document) {
			d.Workflows[0].States[1].Rank = 10
		}},
		{"two complete states", "$.workflows[0].states", "too_many", func(d *policy.Document) {
			d.Workflows[0].States[0].OperatorComplete = true
		}},
		{"self loop", "$.workflows[0].transitions[3]", "self_loop", func(d *policy.Document) {
			d.Workflows[0].Transitions = append(d.Workflows[0].Transitions, policy.Transition{From: "new", To: "new"})
		}},
		{"duplicate transition", "$.workflows[0].transitions[3]", "duplicate", func(d *policy.Document) {
			d.Workflows[0].Transitions = append(d.Workflows[0].Transitions, d.Workflows[0].Transitions[0])
		}},
		{"unknown transition state", "$.workflows[0].transitions[0].to", "unknown_ref", func(d *policy.Document) {
			d.Workflows[0].Transitions[0].To = "ghost"
		}},
		{"unreachable state", "$.workflows[0].states[2]", "unreachable", func(d *policy.Document) {
			d.Workflows[0].Transitions = d.Workflows[0].Transitions[:1]
		}},
		{"routing workflow", "$.routing[0].workflow", "unknown_ref", func(d *policy.Document) { d.Routing[0].Workflow = "ghost" }},
		{"routing level", "$.routing[0].match.level", "unknown_ref", func(d *policy.Document) {
			d.Routing[0].Match.Level = "ghost"
		}},
		{"too many route labels", "$.routing[0].match.labels", "too_many", nineRouteLabels},
		{"default workflow", "$.defaultWorkflow", "unknown_ref", func(d *policy.Document) {
			d.DefaultWorkflow = policy.StringPtr("ghost")
		}},
		{"view window", "$.views.recent", "invalid", func(d *policy.Document) { d.Views.Recent = "2h" }},
		{"empty columns", "$.views.columns", "required", func(d *policy.Document) { d.Views.Columns = nil }},
		{"unknown column", "$.views.columns[0]", "invalid", func(d *policy.Document) { d.Views.Columns[0] = "bogus" }},
		{"duplicate column", "$.views.columns[1]", "duplicate", func(d *policy.Document) { d.Views.Columns[1] = "level" }},
		{"trust minutes low", "$.source.recoveredTrustMinutes", "invalid", func(d *policy.Document) {
			d.Source.RecoveredTrustMinutes = policy.IntPtr(policy.MinRecoveredTrustMinutes - 1)
		}},
		{"trust minutes high", "$.source.recoveredTrustMinutes", "invalid", func(d *policy.Document) {
			d.Source.RecoveredTrustMinutes = policy.IntPtr(policy.MaxRecoveredTrustMinutes + 1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := policy.DefaultDoc()
			tc.mutate(doc)
			issues := policy.Validate(doc)
			if !hasIssue(issues, tc.path, tc.code) {
				t.Fatalf("want issue %s/%s, got %+v", tc.path, tc.code, issues)
			}
		})
	}
}

func TestStrictDecodingAndSizeLimits(t *testing.T) {
	canonical := canonicalDefault(t)
	if _, issues := policy.ParseDocument(canonical); len(issues) != 0 {
		t.Fatalf("canonical default rejected: %+v", issues)
	}
	unknown := append([]byte(`{"note":"x",`), canonical[1:]...)
	if _, issues := policy.ParseDocument(unknown); !hasIssue(issues, "$", "invalid") {
		t.Fatalf("unknown field not rejected: %+v", issues)
	}
	trailing := append(append([]byte{}, canonical...), []byte(`{}`)...)
	if _, issues := policy.ParseDocument(trailing); !hasIssue(issues, "$", "invalid") {
		t.Fatalf("trailing data not rejected: %+v", issues)
	}
	oversized := bytes.Repeat([]byte("x"), policy.MaxDocumentBytes+1)
	if _, issues := policy.ParseDocument(oversized); !hasIssue(issues, "$", "too_long") {
		t.Fatalf("oversized document not rejected: %+v", issues)
	}
	if _, issues := policy.ParseDocument(nil); !hasIssue(issues, "$", "required") {
		t.Fatalf("empty document not rejected: %+v", issues)
	}
}

func TestParseNormalizesOptionalCollections(t *testing.T) {
	raw := `{
		"schemaVersion": 1,
		"severityKey": "severity",
		"levels": [{"id":"only","rank":1,"tone":"grey","name":{"ru":"Один","en":"Only"}}],
		"workflows": [{"id":"basic","name":{"ru":"Базовый","en":"Basic"},"initialState":"new",
			"states":[{"id":"new","rank":1,"name":{"ru":"Новый","en":"New"}}]}],
		"routing": [{"match":{},"workflow":"basic"}],
		"views": {"recent":"24h","history":"7d","columns":["level"]}
	}`
	doc, issues := policy.ParseDocument([]byte(raw))
	if len(issues) != 0 {
		t.Fatalf("issues: %+v", issues)
	}
	if doc.Source.RecoveredTrustMinutes == nil || *doc.Source.RecoveredTrustMinutes != policy.DefaultRecoveredTrustMinutes {
		t.Fatalf("source default = %+v", doc.Source.RecoveredTrustMinutes)
	}
	if doc.Levels[0].Aliases == nil || doc.Workflows[0].Transitions == nil || doc.Routing[0].Match.Labels == nil {
		t.Fatal("optional collections were not normalized")
	}
	canonical, err := policy.CanonicalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"aliases":[]`, `"transitions":[]`, `"labels":{}`, `"recoveredTrustMinutes":60`} {
		if !strings.Contains(string(canonical), fragment) {
			t.Fatalf("canonical document lacks %s: %s", fragment, canonical)
		}
	}
}

func TestCanonicalJSONAndHash(t *testing.T) {
	first, err := policy.CanonicalJSON(policy.DefaultDoc())
	if err != nil {
		t.Fatal(err)
	}
	second, err := policy.CanonicalJSON(policy.DefaultDoc())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("canonical encoding is not deterministic")
	}
	if policy.Hash(first) != policy.Hash(second) {
		t.Fatal("hash is not deterministic")
	}
	changed := policy.DefaultDoc()
	changed.Levels[0].Name.EN = "Catastrophe"
	other, err := policy.CanonicalJSON(changed)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Hash(first) == policy.Hash(other) {
		t.Fatal("hash did not change with the document")
	}
}

func TestRoutingLiteralOrderAndLevelSelector(t *testing.T) {
	doc := policy.DefaultDoc()
	workflows := make([]policy.Workflow, 0, 5)
	for _, id := range []string{"w1", "w2", "w3", "w4", "w5"} {
		workflow := policy.DefaultDoc().Workflows[0]
		workflow.ID = id
		workflows = append(workflows, workflow)
	}
	doc.Workflows = workflows
	doc.Routing = []policy.Route{
		{Match: policy.RouteMatch{Labels: map[string]string{"team": "pay ments"}}, Workflow: "w1"},
		{Match: policy.RouteMatch{Labels: map[string]string{"team": "payments"}}, Workflow: "w2"},
		{Match: policy.RouteMatch{Labels: map[string]string{"empty": ""}}, Workflow: "w3"},
		{Match: policy.RouteMatch{Labels: map[string]string{"Case": "Value"}}, Workflow: "w4"},
		{Match: policy.RouteMatch{Level: "high"}, Workflow: "w5"},
	}
	compiled, err := policy.Compile(doc, 3)
	if err != nil {
		t.Fatalf("compile: %+v", err)
	}
	cases := []struct {
		name    string
		labels  map[string]string
		levelID string
		want    string
		found   bool
	}{
		{"literal space", map[string]string{"team": "pay ments"}, "unknown", "w1", true},
		{"literal no space", map[string]string{"team": "payments"}, "unknown", "w2", true},
		{"case sensitive", map[string]string{"team": "Payments"}, "unknown", "", false},
		{"no trim", map[string]string{"team": " payments"}, "unknown", "", false},
		{"empty value literal", map[string]string{"empty": ""}, "unknown", "w3", true},
		{"missing key is not empty value", map[string]string{}, "unknown", "", false},
		{"case sensitive value", map[string]string{"Case": "value"}, "unknown", "", false},
		{"exact case value", map[string]string{"Case": "Value"}, "unknown", "w4", true},
		{"level selector", map[string]string{}, "high", "w5", true},
		{"level selector mismatch", map[string]string{}, "low", "", false},
		{"first match wins", map[string]string{"team": "payments", "empty": ""}, "unknown", "w2", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := compiled.SelectWorkflow(tc.labels, tc.levelID)
			if got != tc.want || ok != tc.found {
				t.Fatalf("SelectWorkflow = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.found)
			}
		})
	}
}

func TestDefaultWorkflowFallback(t *testing.T) {
	doc := policy.DefaultDoc()
	doc.Routing = nil
	doc.DefaultWorkflow = policy.StringPtr("basic")
	compiled, err := policy.Compile(doc, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := compiled.SelectWorkflow(nil, "unknown"); got != "basic" || !ok {
		t.Fatalf("default workflow = (%q, %v)", got, ok)
	}
	doc.DefaultWorkflow = nil
	compiled, err = policy.Compile(doc, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := compiled.SelectWorkflow(nil, "unknown"); ok {
		t.Fatalf("expected explicit choice, got %q", got)
	}
}

func TestTransitionsAndGates(t *testing.T) {
	compiled, err := policy.Compile(policy.DefaultDoc(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.Transition("basic", "new", "in_progress"); !ok {
		t.Fatal("missing new -> in_progress edge")
	}
	if _, ok := compiled.Transition("basic", "new", "resolved"); ok {
		t.Fatal("unexpected shortcut edge")
	}
	initial, ok := compiled.InitialState("basic")
	if !ok || initial.ID != "new" {
		t.Fatalf("initial state = %+v, %v", initial, ok)
	}
	all := policy.Requires{Ack: true, Owner: true, Comment: true, SourceRecovered: true}
	if missing := policy.MissingGates(all, policy.GateFacts{}); strings.Join(missing, ",") != "ack,owner,comment,sourceRecovered" {
		t.Fatalf("missing gates = %v", missing)
	}
	full := policy.GateFacts{Acknowledged: true, Owner: "alice", Comment: "note", SourceRecovered: true}
	if missing := policy.MissingGates(all, full); len(missing) != 0 {
		t.Fatalf("unexpected missing gates: %v", missing)
	}
	if missing := policy.MissingGates(policy.Requires{Owner: true}, policy.GateFacts{Owner: "   "}); len(missing) != 1 || missing[0] != "owner" {
		t.Fatalf("whitespace owner should not satisfy the gate: %v", missing)
	}
}

func TestMinimumRoleValidationAndDefaults(t *testing.T) {
	doc := policy.DefaultDoc()
	if doc.Workflows[0].MinimumRole != policy.MinimumRoleEditor || doc.Workflows[0].Transitions[0].MinimumRole != policy.MinimumRoleEditor {
		t.Fatalf("default minimum roles = %q / %q", doc.Workflows[0].MinimumRole, doc.Workflows[0].Transitions[0].MinimumRole)
	}

	doc.Workflows[0].MinimumRole = "viewer"
	doc.Workflows[0].Transitions[0].MinimumRole = "owner"
	issues := policy.Validate(doc)
	if !hasIssue(issues, "$.workflows[0].minimumRole", "invalid") {
		t.Fatalf("missing workflow minimumRole issue: %+v", issues)
	}
	if !hasIssue(issues, "$.workflows[0].transitions[0].minimumRole", "invalid") {
		t.Fatalf("missing transition minimumRole issue: %+v", issues)
	}

	valid := policy.DefaultDoc()
	valid.Workflows[0].MinimumRole = policy.MinimumRoleAdmin
	valid.Workflows[0].Transitions[0].MinimumRole = policy.MinimumRoleAdmin
	if issues := policy.Validate(valid); len(issues) != 0 {
		t.Fatalf("admin minimum role rejected: %+v", issues)
	}

	normalized := policy.DefaultDoc()
	normalized.Workflows[0].MinimumRole = ""
	normalized.Workflows[0].Transitions[0].MinimumRole = ""
	policy.Normalize(normalized)
	if normalized.Workflows[0].MinimumRole != policy.MinimumRoleEditor || normalized.Workflows[0].Transitions[0].MinimumRole != policy.MinimumRoleEditor {
		t.Fatal("omitted minimum role did not default to editor")
	}
	if policy.RoleAtLeast("viewer", policy.MinimumRoleEditor) || !policy.RoleAtLeast("editor", policy.MinimumRoleEditor) || !policy.RoleAtLeast("admin", policy.MinimumRoleAdmin) || policy.RoleAtLeast("editor", policy.MinimumRoleAdmin) {
		t.Fatal("role comparison is wrong")
	}
}
