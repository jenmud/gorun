# Recipe: running tasks on your local machine

Demonstrates the Go library convenience API for local execution: `RunLocal`,
`AllServers`, `LocalServer` and `Server.IsLocal`, plus `NewLocalTaskExec`.

The local server runs only the tasks marked `local = true` (`hello`, `workdir`, `show-env`);
the SSH server runs only the rest (`uptime`).

`workdir` has a `check` and a `verify`. The first `go run .` creates the directory and prints
`workdir done`, with the verify's result; run it again and it prints `workdir unchanged`, because
the check found the directory already there. The program reads this from each `TaskEvent`:
`State`, and `Check` and `Verify` for the two commands' results.

## Run

```sh
cd recipes/local-run
go run .
```
