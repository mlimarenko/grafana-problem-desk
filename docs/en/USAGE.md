# Usage

Problem Desk shows problem episodes from Grafana Alerting and keeps the operator record around them: acknowledgement, owner label, comments and activity. Open it from the Grafana main menu.

The workspace is organization-wide. Members of the organization see the same episodes and records; Grafana folder permissions on alert rules do not restrict the plugin. See [security](SECURITY.md).

## Tabs

| Tab | Contents |
| --- | --- |
| Problems | Every active episode, regardless of how long it has been firing. |
| Recent | Active episodes plus episodes recovered within the selected window. The default window is the last 24 hours. |
| History | Episodes whose lifetime overlaps the selected window: started before the end of the window and either still active or recovered after its start. The default window is the last 7 days. |

The Recent and History windows are chosen above the list with bounded start and end values. All three tabs read episodes this installation has observed. Nothing is imported from Grafana's past: pending alerts are not included, and episodes that fired while the plugin was not running are not reconstructed. A History query searches stored episodes; it is not a historical backfill.

## Filters

Filters combine; every counter and page reflects the whole filtered selection.

- **Acknowledgement**: acknowledged, not acknowledged or any.
- **Owner**: exact owner label value.
- **Host**: part of the target host. The host is read from the `host`, then `hostname`, then `instance` label.
- **Severity / importance**: exact configured importance level, selected by level id. The canonical filter key is `severity=<level id>`; the level list comes from the organization policy. A custom source value that has no alias stays visible as the raw value. Sorting follows the configured level rank. Raw source severity is not mapped twice: use the exact label filter for the raw label value.
- **Source state**: active, recovered or any, matching the episode's technical source state.
- **Workflow / operator state**: filter by the pinned workflow and its current operator state. These are distinct from the source-state filter; untracked episodes have no workflow or operator state.
- **Date**: the start and end of the Recent or History window. Problems does not depend on it.
- **Labels**: exact label key and value.
- **Search**: text over the alert fingerprint, labels and annotations.

Changing a filter, a window or the sort order returns the list to its first page. The address bar keeps the selected episode, so a link opens the same inspector for another member of the organization.

Integrations can link to an exact occurrence with `?fingerprint=...&startsAt=...`.
The start time is an RFC3339 instant; the resolver checks both values in the
current Grafana organization and never substitutes a newer occurrence. If the
source has not been observed yet, retry after a successful sync.

A current-problems dashboard can use `?matchers=...`, containing a URL-encoded
JSON object with `__alert_rule_uid__` and `instance`, plus labels such as `device`
or `mountpoint` when needed. This resolves only a unique active occurrence in
the last successful source snapshot. It is not suitable for historical links:
use the episode ID or fingerprint and start time for those. Ambiguous matches
are reported rather than selecting an arbitrary problem.

## Counters, sorting and paging

The backend computes counters, sorting and paging over the full filtered selection, not over the rows already loaded. Counters show the total plus active, recovered, acknowledged, unacknowledged, unassigned and a count for each configured importance level. Filter lists for hosts, owners and label keys offer the values present in the current tab.

Sortable columns are importance (severity), start time, duration and recovery time, in either direction. Duration is calculated when the table is displayed: for an active episode it runs to the current time, for a recovered episode to its recovery. Page size is 25, 50 or 100 rows. The organization policy supplies the default Recent and History windows and the visible columns; an explicit window in the address overrides the default.

## Inspector

Select a problem title to open the inspector. It has four parts.

- **Summary**: source state, importance level, target, start time, last seen time, recovery time, owner label and acknowledgement, with the acknowledging user and time.
- **Labels and annotations**: values exactly as Grafana Alerting delivered them. They are never translated or rewritten; only the owner label belongs to Problem Desk.
- **Workflow**: the pinned process and its current state, the transitions the organization policy allows, and the requirements still missing. An episode starts untracked; start it explicitly to opt into a process. Workflow actions never change the source `active`/`recovered` state. If the source becomes active again while the workflow is complete, the inspector shows the conflict and offers the configured reopen transition.
- **Discussion**: a plain-text comment form that preserves literal characters.
- **Activity**: comments and operator events in one newest-first timeline, including acknowledgement, removed acknowledgement, owner changes, recovery, workflow start and workflow transitions. Every entry retains its author, timestamp and the policy revision used.

The target line prefers the alert's host metadata over an address. The generator link opens the Grafana source only when it is a valid `http` or `https` URL. Displayed times use the browser's timezone.

Use **Export page CSV** to download the currently loaded page, not the full
filtered result. Values that could be interpreted as spreadsheet formulas are
escaped on export. The time-zone control switches between local time and UTC.

## Work with an episode

1. Choose a tab and narrow the list with filters.
2. Select the problem title. The inspector replaces the previous selection; unsent comment text is confirmed before it is discarded.
3. Write in the comment field and add the comment.
4. Select **Acknowledge** when the problem has been seen. **Remove acknowledgement** records a separate activity event.
5. Set the **Owner label** to a login or team name. It is a collaboration label, not user provisioning or a permission grant.
6. Open the **Workflow** section to start the process selected by the organization policy, or to advance the pinned process. When a routing rule or default selects a process, that process is fixed and cannot be replaced from the drawer. The inspector lists the allowed next states, the minimum role and the acknowledgement, owner, comment or recovered-source requirements still unmet. Admin-only actions are disabled for Editors. A transition that requires a comment stores the comment with the transition. If another operator or a policy edit changed the case after the drawer loaded, the action is rejected as stale and the drawer refreshes instead of retrying silently.

A recovered episode keeps its discussion and activity. A later occurrence of the same alert is a separate episode with its own discussion and acknowledgement. There are no bulk actions; change one episode at a time.

## Policy and administrators

Administrators edit the organization policy in the app configuration page. Levels, workflows, routing rules, view defaults and the source-recovery trust window are validated before saving; a stale revision is rejected rather than overwriting another administrator's change. Every accepted revision is kept in the audit history and can be loaded as a new draft. Validation errors point at the exact field path. The editor also previews the level and workflow selected for sample labels, including unmet start requirements. JSON import and export are available for advanced cases and never include credentials.

Routing rules use exact label values only. There are no scripts, regular expressions, webhooks or metric thresholds. The policy cannot grant Viewer writes or change Grafana authorization.

## Language

The page header has a Russian and English switch. The choice is stored in this browser and survives reloads; the first visit follows the Grafana or browser language. The switch translates the plugin interface only: alert labels, annotations, comments and owner values remain as entered.

## Roles

| Role | Permitted |
| --- | --- |
| Viewer | Read episodes, discussion, activity and policy explanations. |
| Editor | Everything a Viewer can do, plus comment, acknowledge, remove acknowledgement, set the owner label and start or advance an episode workflow when the policy allows it. |
| Admin | Everything an Editor can do, plus manual source sync, app configuration and organization policy editing. |

## Boundaries

Acknowledgement is a collaboration record, not a Grafana Alerting silence. It does not resolve the alert, stop rule evaluation or mute notifications; manage rules, routing and silences in Grafana Alerting. A workflow transition is an operator record with the same boundary: completion does not resolve, suppress or hide an active source episode.

Episodes, comments and activity records cannot be deleted from the plugin. Problem Desk does not evaluate alert rules and does not import pending or earlier alerts.
