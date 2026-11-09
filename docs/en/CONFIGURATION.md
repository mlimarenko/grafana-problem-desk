# Configuration

**Problem Desk is an organization-wide shared workspace.** It does not reproduce per-folder Grafana Alerting access controls. Alert data visible to the configured service account is shared with organization members through the plugin. Administrators must not enable the app where this would expose restricted alert data.

An administrator configures the app with `jsonData.grafanaUrl` and `jsonData.pollIntervalSeconds` (default `30`), and sets `secureJsonData.grafanaToken` to an organization-scoped Grafana service-account token with read access to alerts. The backend checks `/api/org` against the trusted Grafana plugin organization ID. Do not put the token in ordinary JSON configuration or share it in logs.

The backend persists data in SQLite, separate from the Grafana database. `PROBLEMDESK_DATA_PATH` defaults to `/var/lib/grafana/problem-desk`; put this directory on persistent storage, protect it, and include it in backup and restore procedures.

The source is the Grafana Alertmanager API's active firing alerts, not pending alerts. Polling begins on the first authenticated app visit after the plugin process starts and continues until the instance is disposed. Events missed during downtime cannot be recovered; there is no historical backfill. An episode is marked recovered only when a successful complete snapshot omits it, not on a network error.

## Views and windows

The three views need no configuration. **Problems** always lists every active episode. **Recent** adds episodes recovered within the selected window and defaults to the last 24 hours. **History** lists episodes overlapping the selected window and defaults to the last 7 days. Members choose the window with bounded start and end values in the page; these choices are not server settings.

## Upgrade from 0.1.0

Episodes, comments, acknowledgements and activity recorded by 0.1.0 remain available after the upgrade. No manual data migration step is required. Back up `PROBLEMDESK_DATA_PATH` before upgrading, as for any release.

## Language

There is no server-side language setting. Each member switches the interface between Russian and English in the page header; the choice is stored in that browser and defaults to the Grafana or browser language on first use. Alert labels, annotations, comments and owner values are never translated.

## Access

Viewers read the workspace. Editors and administrators comment, acknowledge, remove acknowledgement and set the owner label. Administrators also run manual source sync and change this configuration. The plugin does not delete episodes, comments or activity records.

See [installation](INSTALL.md) for the unsigned-plugin setting and [security](SECURITY.md) for operational precautions.
