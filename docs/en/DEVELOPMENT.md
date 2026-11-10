# Development

The plugin combines a Grafana-native frontend with a Go backend. The project entry points are `make build`, `make test`, `make dev` and `make package` for building, testing, local development and packaging respectively. The Docker Compose Grafana demo binds to localhost port `13010`.

The backend ownership is split by responsibility: `pkg/policy` owns the declarative document schema, validation, compilation and level/workflow evaluation; `pkg/storage` owns the additive schema migration, immutable policy revisions, CAS writes and workflow persistence; `pkg/plugin` owns the org-scoped resource routes and the trusted actor/role boundary; `pkg/sync` remains the Grafana source owner. Policy documents are validated before storage, and every policy-governed write reads the active revision in the same SQLite transaction as its state and audit rows.

Use a private Grafana development instance when loading an unsigned build. See [installation](INSTALL.md) for the narrowly scoped Grafana setting. Before sharing logs or fixtures, remove credentials and sensitive alert data.

See [contributing](../../CONTRIBUTING.md) for reporting issues and proposing changes.
