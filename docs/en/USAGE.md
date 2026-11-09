# Usage

Open Problem Desk in Grafana to work with active firing alerts from Grafana Alertmanager. The first authenticated visit starts polling after the plugin process starts. Events not observed during downtime are not backfilled.

An episode is identified by organization, fingerprint and start time. A repeated firing is a distinct episode with its own comments and acknowledgements. Recovery is recorded only when a successful complete source snapshot omits the episode; a network error does not imply recovery.

Viewers can read the workspace. Editors and administrators can comment, acknowledge or unacknowledge episodes, and set owner labels. Administrators control configuration and manual sync. The workspace shares service-account-visible alert data across the organization; see [security](SECURITY.md).

Acknowledgement is a collaboration record, not a Grafana Alerting silence. It does not resolve the alert, stop evaluation or automatically mute notifications. Manage alert rules, notification routing and silences in Grafana Alerting.

## Work with an episode

1. Choose Active, Recovered or All episodes. Filter by severity, or search for a host, rule or label. Custom severity values are accepted.
2. Select the problem title to open its details. The page URL includes the episode ID and can be shared with another authorized member of the same organization.
3. Write in **Comment on this episode** and select **Add comment**. Comments are plain text, including literal code characters, with the authenticated Grafana user's name and timestamp.
4. Select **Acknowledge** to record that the problem has been seen. **Remove acknowledgement** records a separate activity event.
5. Set **Owner label** to a login or team name. This is a collaboration label, not user provisioning or a permission grant.

The discussion remains available when the episode recovers. A later occurrence of the same alert gets a separate discussion. Displayed times use the browser's local timezone. Pagination applies to the filtered result, not just the currently loaded rows.
