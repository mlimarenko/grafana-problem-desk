# Security and operations

**Problem Desk is an organization-wide shared workspace, not a per-folder access boundary.** It does not reproduce Grafana Alerting's per-folder ACLs. Alert data visible to the configured service account is shared with organization members through the plugin. An administrator must not enable the app where that would reveal restricted alert data.

Viewers can read; Editors and Admins can comment, ACK or unACK and advance configured workflows; Admins can configure, edit the organization policy and manually sync. The backend derives the actor from the trusted Grafana SDK user, not a request payload. Configure an organization-scoped service account with read access to alerts; the backend verifies its organization against the trusted plugin organization ID.

The organization policy is validated server-side and stored as immutable per-organization revisions with an append-only audit. Only Admins can write it; it never grants Viewer mutations or changes Grafana authorization, and it cannot carry credentials or execute scripts, regular expressions, webhooks or notification rules. Workflow and level metadata never rewrite the source `active`/`recovered` truth.

The assignee list is a separate organization-scoped snapshot explicitly
published by an Admin from Grafana's user directory. Editors and Admins may
read it; Viewers may not. Only UID, login and name are published, not email or
permissions. Refresh does not grant the source account directory privileges.
New assignments are checked against the current published list transactionally;
existing assignment labels are not access grants and are not erased by refresh.

Comments and acknowledgements are stored in organization-scoped persistent SQLite, separate from Grafana's database. Treat storage and backups as sensitive: comments and alert context may contain operational details. Restrict access, protect backups and apply retention rules appropriate to your environment. Keep the service-account token in secure plugin configuration.

An acknowledgement does not mute or resolve an alert. The plugin is unsigned; allow it only by the explicit plugin ID in self-hosted development.

For vulnerability reporting, see the [security policy](../../SECURITY.md).
