# Development Scripts

This directory contains scaffolding and utility scripts for the KubeStateMetrics service provider.

## Contents

- `boilerplate.go.txt` — Go source file header template (copyright/license boilerplate)
- `common/` — Shared template scripts from the service provider scaffold

## Testing

The primary way to test this service provider is via the E2E test suite:

```bash
# Run E2E tests (creates kind clusters, deploys operator, verifies end-to-end)
make test-e2e
```

See the project [README.md](../README.md) for full testing instructions.

## Additional Resources

- **Examples**: See [`examples/`](../examples/) for configuration examples
- **E2E Tests**: See [`test/e2e/`](../test/e2e/) for automated test suite
