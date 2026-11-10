package storage

import (
	"context"
	"testing"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/policy"
)

func TestListingOrderTieAndRetiredLevelSummary(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	doc := cloneDoc(t, policy.DefaultDoc())
	name := policy.LocalizedText{RU: "Имя", EN: "Name"}
	doc.Levels = []policy.Level{
		{ID: "gamma", Rank: 5, Tone: "red", Name: name, Aliases: []string{"gamma"}},
		{ID: "beta", Rank: 3, Tone: "blue", Name: name, Aliases: []string{"beta"}},
		{ID: "alpha", Rank: 3, Tone: "blue", Name: name, Aliases: []string{"alpha"}},
		{ID: "unknown", Rank: 1, Tone: "grey", Name: name, Aliases: []string{}},
	}
	doc.Routing = nil
	if _, err := store.PutPolicy(ctx, 1, "admin", 1, doc); err != nil {
		t.Fatal(err)
	}

	syncActiveAt(t, store, 1, now,
		SnapshotAlert{Fingerprint: "fp-gamma", StartsAt: now.Format(time.RFC3339Nano), Labels: map[string]string{"severity": "gamma"}},
		SnapshotAlert{Fingerprint: "fp-beta", StartsAt: now.Format(time.RFC3339Nano), Labels: map[string]string{"severity": "beta"}},
		SnapshotAlert{Fingerprint: "fp-alpha", StartsAt: now.Format(time.RFC3339Nano), Labels: map[string]string{"severity": "alpha"}},
	)

	desc, err := store.GetProblems(ctx, 1, ProblemQuery{View: "problems", State: "all", Sort: "severity", Order: "desc", Now: now, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(desc.Problems) != 3 || desc.Problems[0].LevelID != "gamma" || desc.Problems[1].LevelID != "alpha" || desc.Problems[2].LevelID != "beta" {
		t.Fatalf("tie order = %+v", desc.Problems)
	}
	if desc.Summary.ByLevel["gamma"] != 1 || desc.Summary.ByLevel["alpha"] != 1 || desc.Summary.ByLevel["beta"] != 1 {
		t.Fatalf("summary = %+v", desc.Summary.ByLevel)
	}

	asc, err := store.GetProblems(ctx, 1, ProblemQuery{View: "problems", State: "all", Sort: "severity", Order: "asc", Now: now, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(asc.Problems) != 3 || asc.Problems[0].LevelID != "alpha" || asc.Problems[1].LevelID != "beta" || asc.Problems[2].LevelID != "gamma" {
		t.Fatalf("ascending order = %+v", asc.Problems)
	}

	// A retired id stays visible in the filtered summary and remains filterable.
	if _, err := store.db.ExecContext(ctx, `UPDATE problems SET level_id = 'legacy' WHERE org_id = 1 AND fingerprint = 'fp-beta'`); err != nil {
		t.Fatal(err)
	}
	retired, err := store.GetProblems(ctx, 1, ProblemQuery{View: "problems", State: "all", Sort: "severity", Order: "desc", Now: now, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if retired.Summary.ByLevel["legacy"] != 1 || retired.Summary.ByLevel["beta"] != 0 {
		t.Fatalf("retired summary = %+v", retired.Summary.ByLevel)
	}
	if retired.Problems[len(retired.Problems)-1].LevelID != "legacy" {
		t.Fatalf("retired level must sort last: %+v", retired.Problems)
	}
	filtered, err := store.GetProblems(ctx, 1, ProblemQuery{View: "problems", State: "all", Severity: "legacy", Sort: "severity", Order: "desc", Now: now, Limit: 10})
	if err != nil || filtered.Total != 1 || filtered.Problems[0].LevelID != "legacy" {
		t.Fatalf("retired filter = %+v, %v", filtered, err)
	}
}
