# Changelog

## 0.3.1

- Choose an assignee from a searchable organization participant list instead of typing a login.
- Administrators explicitly refresh the published list from Grafana; Editors do not need user-directory privileges and the source token stays read-only.
- Preserve existing assignments and history when a participant leaves the published list.

## 0.3.0

- Organization policy: validated, revisioned levels, workflows, label routing and view defaults per Grafana organization, with optimistic revision checks and an immutable audit history.
- Configurable importance levels with stable ids, Russian and English names, bounded theme colors and ranks, source-label aliases and a configurable source severity key. Defaults reproduce the 0.2 ordering exactly, including `disaster`, `critical`, `high`, the equal-rank `warning`/`average`, `info`/`information` and the unknown fallback.
- Optional operator workflows with stable states, explicit start and transitions, gates for acknowledgement, owner, comment and a recovered source state confirmed by a trusted snapshot. Workflow identity is pinned per episode; routing edits do not move existing cases. Completion is separate from graph termination and an audited reopen edge can be configured. Every workflow start and transition has a configurable minimum native role: Editor by default, optionally Admin. The policy never grants Viewer mutations or changes Grafana authorization.
- Start and transition requests carry the expected policy revision, and transitions also carry the expected current operator state. The server compares both inside the writer transaction and returns a conflict with zero state, event or comment writes if either changed, even when the new graph would allow the same target.
- Exact label rules select an organizational processing profile; no scripts, regular expressions, webhooks or metric thresholds. Administrators preview the effective profile and unmet requirements. When a rule or non-null default selects a workflow, the server enforces that choice and does not let an operator substitute a weaker workflow; explicit choice is offered only when nothing selects one.
- Native form-based policy editor with validation errors by JSON path, optimistic revision conflict handling, revision history and bounded JSON import/export.
- Listing API and UI now read level metadata from the server: `levelId`, `levels`, `severityKey`, `policyRevision`, `summary.byLevel`, the `severity=<configured level id>` filter and `severity` rank sort, plus distinct `workState` and `workflow` operator filters. The duplicated hardcoded severity classification was removed.
- Additive schema migration preserves 0.2 episodes, comments, acknowledgements, owners and events; legacy episodes stay untracked until an operator explicitly starts a workflow.

## 0.2.2

Interpret Grafana runtime states with informational reasons, including `Normal (NoData)`, without blocking synchronization of valid snapshots.

## 0.2.1

Keep existing problem states while Grafana's alerting runtime is initializing, rather than interpreting its temporary empty Alertmanager response as recovery.

## 0.2.0

- Problems, Recent and History views: all active episodes, active plus recently recovered episodes (default window 24 hours), and episodes overlapping a bounded window (default 7 days).
- Filters for acknowledgement, owner, host, severity, date, labels and text search.
- Backend counters over the full filtered selection, sortable columns and paging at 25, 50 or 100 rows.
- Episode inspector with original labels and annotations, discussion, and a separate operator activity log.
- Russian and English interface with a browser-persisted language switch.
- Acknowledgements, comments and activity from 0.1.0 are kept. The source remains Grafana-managed Alertmanager, with no pending alerts and no historical backfill.

## 0.1.0

Initial version: Grafana app plugin for problem episodes, acknowledgements and comments.
