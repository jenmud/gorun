# Recipe: running tasks on your local machine

Demonstrates the Go library convenience API for local execution: `RunLocal`,
`AllServers`, `LocalServer` and `Server.IsLocal`, plus `NewLocalTaskExec`.

The local server runs only the tasks marked `local = true` (`hello`, `show-env`); the SSH
server runs only the rest (`uptime`).

## Run

```sh
cd recipes/local-run
go run .
```
