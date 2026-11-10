# Configuration

**Problem Desk is an organization-wide shared workspace.** It does not reproduce per-folder Grafana Alerting access controls. Alert data visible to the configured service account is shared with organization members through the plugin. Administrators must not enable the app where this would expose restricted alert data.

An administrator configures the app with `jsonData.grafanaUrl` and `jsonData.pollIntervalSeconds` (default `30`), and sets `secureJsonData.grafanaToken` to an organization-scoped Grafana service-account token with read access to alerts. The backend checks `/api/org` against the trusted Grafana plugin organization ID. Do not put the token in ordinary JSON configuration or share it in logs.

The backend persists data in SQLite, separate from the Grafana database. `PROBLEMDESK_DATA_PATH` defaults to `/var/lib/grafana/problem-desk`; put this directory on persistent storage, protect it, and include it in backup and restore procedures.

The source is the Grafana Alertmanager API's active firing alerts, not pending alerts. Polling begins on the first authenticated app visit after the plugin process starts and continues until the instance is disposed. Events missed during downtime cannot be recovered; there is no historical backfill. An episode is marked recovered only when a successful complete snapshot omits it, not on a network error.

The source account also needs organization-wide read access to configured and
evaluated Grafana alert rules. Problem Desk checks the ruler inventory and
runtime evaluations before accepting an Alertmanager snapshot as complete.
During Grafana startup or delayed notification delivery, existing episodes stay
unchanged and the source reports that it is not ready. A verified empty snapshot
after rules recover or are deleted can still recover the last active episode.
Restricted folder visibility cannot establish complete organization-wide rule
inventory; do not configure such a token as the source for this workspace.

## Organization policy

Each Grafana organization has its own validated policy document. The policy is stored in Problem Desk's SQLite database as immutable revisions; the newest revision is active. The source configuration and its token stay in Grafana secure settings and are not duplicated in the policy.

An administrator edits the policy in the app configuration page with native Grafana controls. Saving requires an administrator role and the current revision number: a concurrent change is rejected instead of overwritten, and every accepted revision is recorded in an append-only audit. The editor validates the document and can preview the effective level and workflow for sample labels. Validation errors point at the exact JSON path. JSON import and export are available for advanced use and never include credentials.

- **Levels** have a stable id, rank, a bounded theme color, Russian and English names and aliases for source severity values. `severityKey` selects the source label (default `severity`; another organization may use `priority`). Raw labels are never rewritten. Defaults reproduce the 0.2 order exactly: `disaster`, `critical`, `high`, `warning`/`average`, `info`/`information`, then the unknown fallback. Equal ranks are allowed and ties are stable.
- **Workflows** have stable state ids, states, an explicit initial state and explicit transitions. A start or transition can require acknowledgement, an owner label, a comment or a recovered source state, and each start or transition sets a minimum native role: Editor by default, optionally Admin. Workflow changes require Editor or Admin as that minimum dictates; a policy never grants Viewer writes or changes Grafana authorization. A state may be marked as operator-complete, but completion is not graph termination: an explicit, audited reopen transition can be configured.
- **Routing** selects a workflow from exact source label values (and optionally a derived level). Rules are evaluated in the listed order; there are no regular expressions, scripts, webhooks or metric thresholds. When a rule or a non-null default workflow selects a workflow, that selection is enforced by the server: an operator cannot substitute another, weaker workflow id. Operator choice is offered only when no rule or default selects one. An episode's workflow is pinned when an operator starts it; later routing edits do not move existing cases.
- **Optimistic actions** carry the policy revision the operator saw; a transition also carries the current operator state. The server compares both with the stored row inside the same write transaction. If either changed, the action is rejected with a conflict and writes no state, event or comment, even when the new graph would allow the same target.
- **Views** set the default Recent and History windows and the visible table columns. Allowed windows are 1 hour, 24 hours, 7 days and 30 days. An explicit `range` in the page address overrides the organization default.
- **Source recovery** sets how long a successful source snapshot is trusted for the `sourceRecovered` transition gate, from 5 to 1440 minutes (default 60). The gate also fails closed when the last source validation failed or no sync has succeeded in the current process.

Workflow identity and level classification are evaluation metadata. They never change an episode's source `active`/`recovered` state, and a human completion never hides an active alert or suppresses Grafana notifications. All active source episodes stay visible in the Problems view.

## Views and windows

**Problems** always lists every active episode. **Recent** adds episodes recovered within the selected window. **History** lists episodes overlapping the selected window. The organization policy supplies the default Recent and History windows (0.2 defaults: 24 hours and 7 days); members can still choose another bounded window in the page, and an explicit choice in the address overrides the default. Table columns also come from the policy and are bounded to the existing set.

## Upgrade from 0.2

Episodes, comments, acknowledgements, owner labels and activity recorded by 0.1.0 and 0.2 remain available after the upgrade. The schema migration is additive; legacy episodes stay untracked until an operator explicitly starts a workflow, and no synthetic completion, approval or level change is invented. Back up `PROBLEMDESK_DATA_PATH` with an online backup before upgrading, as for any release.

A 0.2 binary cannot enforce 0.3 workflow gates, so downgrade is not a safe rollback. To recover from a bad configuration, write an earlier document as a new revision (rollback-forward) and keep the human workflow actions recorded since the upgrade. Restoring the pre-upgrade backup is a named disaster path that discards those actions and requires explicit operator acknowledgement.

## Language

There is no server-side language setting. Each member switches the interface between Russian and English in the page header; the choice is stored in that browser and defaults to the Grafana or browser language on first use. Alert labels, annotations, comments and owner values are never translated.

## Access

Viewers read the workspace and the active policy explanation. Editors and administrators comment, acknowledge, remove acknowledgement, set the owner label and start or advance an episode workflow when the policy allows it. Administrators also run manual source sync, change the source configuration and edit the organization policy. The plugin does not delete episodes, comments or activity records.

Before participants can be newly assigned, an Admin publishes the assignee list
with **Update participants** on the Problem Desk page. Repeat after organization
membership changes. This uses the administrator's native Grafana session, not
the alert source token. Editors can read the published list and assign from it;
Viewers cannot read the directory or change assignments. See
[assignee selection](USAGE.md) for refresh errors and existing assignments.

See [installation](INSTALL.md) for the unsigned-plugin setting and [security](SECURITY.md) for operational precautions.
