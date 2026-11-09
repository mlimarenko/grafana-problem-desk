# Development

The plugin combines a Grafana-native frontend with a Go backend. The project entry points are `make build`, `make test`, `make dev` and `make package` for building, testing, local development and packaging respectively. The Docker Compose Grafana demo binds to localhost port `13010`.

Use a private Grafana development instance when loading an unsigned build. See [installation](INSTALL.md) for the narrowly scoped Grafana setting. Before sharing logs or fixtures, remove credentials and sensitive alert data.

See [contributing](../../CONTRIBUTING.md) for reporting issues and proposing changes.
