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
- **Severity**: exact severity label, including custom values. Sorting uses a severity order, so critical and disaster rank above warning and info.
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

The backend computes counters, sorting and paging over the full filtered selection, not over the rows already loaded. Counters show the total plus active, recovered, acknowledged, unacknowledged, unassigned, critical and warning subsets. Filter lists for hosts, owners and label keys offer the values present in the current tab.

Sortable columns are severity, start time, duration and recovery time, in either direction. Duration is calculated when the table is displayed: for an active episode it runs to the current time, for a recovered episode to its recovery. Page size is 25, 50 or 100 rows.

## Inspector

Select a problem title to open the inspector. It has four parts.

- **Summary**: state, severity, target, start time, last seen time, recovery time, owner label and acknowledgement, with the acknowledging user and time.
- **Labels and annotations**: values exactly as Grafana Alerting delivered them. They are never translated or rewritten; only the owner label belongs to Problem Desk.
- **Discussion**: a plain-text comment form that preserves literal characters.
- **Activity**: comments and operator events in one newest-first timeline, including acknowledgement, removed acknowledgement, owner changes and recovery. Every entry retains its author and timestamp.

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

A recovered episode keeps its discussion and activity. A later occurrence of the same alert is a separate episode with its own discussion and acknowledgement. There are no bulk actions; change one episode at a time.

## Language

The page header has a Russian and English switch. The choice is stored in this browser and survives reloads; the first visit follows the Grafana or browser language. The switch translates the plugin interface only: alert labels, annotations, comments and owner values remain as entered.

## Roles

| Role | Permitted |
| --- | --- |
| Viewer | Read episodes, discussion and activity. |
| Editor | Everything a Viewer can do, plus comment, acknowledge, remove acknowledgement and set the owner label. |
| Admin | Everything an Editor can do, plus manual source sync and app configuration. |

## Boundaries

Acknowledgement is a collaboration record, not a Grafana Alerting silence. It does not resolve the alert, stop rule evaluation or mute notifications; manage rules, routing and silences in Grafana Alerting.

Episodes, comments and activity records cannot be deleted from the plugin. Problem Desk does not evaluate alert rules and does not import pending or earlier alerts.
