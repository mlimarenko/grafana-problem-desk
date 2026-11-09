# Security and operations

**Problem Desk is an organization-wide shared workspace, not a per-folder access boundary.** It does not reproduce Grafana Alerting's per-folder ACLs. Alert data visible to the configured service account is shared with organization members through the plugin. An administrator must not enable the app where that would reveal restricted alert data.

Viewers can read; Editors and Admins can comment and ACK or unACK; Admins can configure and manually sync. The backend derives the actor from the trusted Grafana SDK user, not a request payload. Configure an organization-scoped service account with read access to alerts; the backend verifies its organization against the trusted plugin organization ID.

Comments and acknowledgements are stored in organization-scoped persistent SQLite, separate from Grafana's database. Treat storage and backups as sensitive: comments and alert context may contain operational details. Restrict access, protect backups and apply retention rules appropriate to your environment. Keep the service-account token in secure plugin configuration.

An acknowledgement does not mute or resolve an alert. The first version is unsigned; allow it only by the explicit plugin ID in self-hosted development.

For vulnerability reporting, see the [security policy](../../SECURITY.md).
