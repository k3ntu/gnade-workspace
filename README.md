# gnade-workspace

Shared Go persistence library for GnadeDoc Graph and GnadeBoard Kanban.

It defines workspace manifests, projects and version snapshots, and implements
JSON persistence, CRUD, trash/restore and snapshot operations.

## Usage

Import `github.com/k3ntu/gnade-workspace` (package name: `gnadeworkspace`).
Create a store with `NewStore(path)`; an empty path defaults to
`~/.gnade/workspaces.json` outside tests. Applications can supply a separate
development path.

The library is compiled into each application. No separate service or runtime
installation is required. Applications share persisted data only when they open
the same file under the same user account. The mutex protects one Store instance;
it does not provide locking between application processes.

## Validation

```sh
go build ./...
go vet ./...
go test ./...
```
