# Configuration

**Problem Desk is an organization-wide shared workspace.** It does not reproduce per-folder Grafana Alerting access controls. Alert data visible to the configured service account is shared with organization members through the plugin. Administrators must not enable the app where this would expose restricted alert data.

An administrator configures the app with `jsonData.grafanaUrl` and `jsonData.pollIntervalSeconds` (default `30`), and sets `secureJsonData.grafanaToken` to an organization-scoped Grafana service-account token with read access to alerts. The backend checks `/api/org` against the trusted Grafana plugin organization ID. Do not put the token in ordinary JSON configuration or share it in logs.

The backend persists data in SQLite, separate from the Grafana database. `PROBLEMDESK_DATA_PATH` defaults to `/var/lib/grafana/problem-desk`; put this directory on persistent storage, protect it, and include it in backup and restore procedures.

Polling begins on the first authenticated app visit after the plugin process starts and continues until the instance is disposed. The source is the Grafana Alertmanager API's active firing alerts, not pending alerts. Events missed during downtime cannot be recovered; there is no historical backfill. An episode is marked recovered only when a successful complete snapshot omits it, not on a network error.

See [installation](INSTALL.md) for the unsigned-plugin setting and [security](SECURITY.md) for operational precautions.
