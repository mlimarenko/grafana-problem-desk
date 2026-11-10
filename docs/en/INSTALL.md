# Installation

Problem Desk is an app plugin for Grafana 13.3 or later. Install a plugin package on the Grafana server, then enable the app in Grafana. The plugin ID is `mlimarenko-problemdesk-app`. The initial binary package targets Linux amd64.

The plugin is unsigned. For self-hosted development only, Grafana can be configured to allow this specific unsigned plugin using `allow_loading_unsigned_plugins = mlimarenko-problemdesk-app` in its `[plugins]` configuration. Restart Grafana after changing its configuration. Do not use a blanket unsigned-plugin allowance.

Download the Linux amd64 package and verify its checksum before unpacking it:

```sh
VERSION=0.3.0
ASSET="mlimarenko-problemdesk-app-${VERSION}.zip"
BASE="https://github.com/mlimarenko/grafana-problem-desk/releases/download/v${VERSION}"
curl -fLO "${BASE}/${ASSET}"
curl -fLO "${BASE}/${ASSET}.sha256"
sha256sum -c "${ASSET}.sha256"
sudo unzip "${ASSET}" -d /var/lib/grafana/plugins
```

Use your Grafana installation's configured plugin directory if it differs. The package must remain readable by the Grafana service user, and `gpx_problemdesk_linux_amd64` must be executable. Mount the plugin directory into the container when using Docker. The backend's persistent data directory is separate from the plugin files; do not put it in a temporary container layer.

Enable the app in Grafana's plugin administration and open Problem Desk from Grafana. Access to alert data and persistent storage must be configured for the deployment; see [configuration](CONFIGURATION.md) and [security](SECURITY.md).
