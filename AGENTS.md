# VLNO SDK and CLI

Customer clients for VLNO hosted evaluations. Keep engine implementation, private
scenario data, infrastructure configuration and credentials out of this repository.

## Stack and checks

- Python 3.11+, no runtime dependencies; distribution `vlno-sdk`, import `vlno`.
- Go CLI; executable `vlno`.
- Python: `PYTHONPATH=python/src:python/tests python -m unittest discover -s python/tests`.
- CLI: `cd cli && go test ./...`.
- Release archives must contain only client code, public examples and documentation.

Use `main`; never commit credentials. Account credentials belong in the trusted
controller, and only scoped connections are handed to the evaluated harness.
