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

Use the filter icon in a column header to narrow the list. The Problem column filters by title and by host; Started and Duration accept one or two bounds. The Owner column filters by exact owner or by whether an owner is assigned. The Status filter is available in Recent and History; Problems always shows active episodes.

**Filters** contains workflow, workflow state, exact labels, and filters for hidden columns. The search field searches fingerprints, labels and annotations. To filter on the source severity, use its exact `severity` label.

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

The backend filters and sorts the whole selection before paging. The total is below the table. Turn on **View → Summary** to see the breakdown by status and severity.

Select any heading to sort by that column; select it again to reverse the order. In Recent and History, the Status menu also offers sorting by recovery time. Duration runs from start to recovery, or to the current time for an active episode. Page size is 25, 50 or 100 rows. The organization policy supplies the default Recent and History windows and the visible columns; an explicit window in the address overrides the default.

## Episode details

Select the problem title in the table. The drawer shows the host, status, description and comments.

- Select **Assign to me** to claim an unassigned episode. **Release** clears only your own assignment.
- **Acknowledge** records that you have seen the problem. Notifications continue. Select **Undo** next to the acknowledgement to remove it.
- Write a comment and select **Send**. Comments appear newest first.

In **More**, you can open the **Alert rule**, copy the link and turn on extra sections:

- **Workflow** shows the steps allowed by the organization policy
- **Activity log** adds acknowledgements, assignments and workflow steps to the comments
- **Alert details** shows severity, times, ID, labels and annotations

Your browser remembers the display options. They do not change any records. **Export page CSV** exports the current filtered page and escapes values that might be interpreted as spreadsheet formulas.

A recovered episode keeps its discussion and activity. A later occurrence of the same alert is a separate episode with its own discussion and acknowledgement. There are no bulk actions; change one episode at a time.

## Policy and administrators

Administrators edit the organization policy in the app configuration page. Levels, workflows, routing rules, view defaults and the source-recovery trust window are validated before saving; a stale revision is rejected rather than overwriting another administrator's change. Every accepted revision is kept in the audit history and can be loaded as a new draft. Validation errors point at the exact field path. The editor also previews the level and workflow selected for sample labels, including unmet start requirements. JSON import and export are available for advanced cases and never include credentials.

Routing rules use exact label values only. There are no scripts, regular expressions, webhooks or metric thresholds. The policy cannot grant Viewer writes or change Grafana authorization.

## Language and time

Use **View** to choose the interface language and local time or UTC. The initial language follows Grafana or the browser. Alert text and comments are not translated.

## Roles

| Role | Permitted |
| --- | --- |
| Viewer | Read episodes, discussion, activity and policy explanations. |
| Editor | Comment, acknowledge, claim an unassigned episode, release their own assignment and run workflows within the organization policy. |
| Admin | Everything an Editor can do, plus manual source sync, app configuration and organization policy editing. |

## Boundaries

Acknowledgement is a collaboration record, not a Grafana Alerting silence. It does not resolve the alert, stop rule evaluation or mute notifications; manage rules, routing and silences in Grafana Alerting. A workflow transition is an operator record with the same boundary: completion does not resolve, suppress or hide an active source episode.

Episodes, comments and activity records cannot be deleted from the plugin. Problem Desk does not evaluate alert rules and does not import pending or earlier alerts.
