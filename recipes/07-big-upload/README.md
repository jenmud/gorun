# Recipe 7: big uploads

Uploads a 1 GB release and a 200-file static site to three lab servers, and shows how far each
upload has got: a bar, the percentage, the bytes sent and the speed.

## Run it

Start the lab first (see [the recipes README](../README.md)), then make the files to upload. They
go in `build/`, which git ignores:

```sh
recipes/07-big-upload/make-files.sh              # build/release.tar.gz is 1 GB
SIZE_MB=4096 recipes/07-big-upload/make-files.sh # or 4 GB, for a longer upload
```

Then run it:

```sh
gorun recipes/07-big-upload/Gorunfile -r         # the live view
gorun recipes/07-big-upload/Gorunfile -r         # again: nothing is uploaded, it is all there
gorun recipes/07-big-upload/Gorunfile -r --check # dry run: which uploads would be sent
gorun recipes/07-big-upload/Gorunfile -r --force # upload everything again anyway
gorun recipes/07-big-upload/Gorunfile -r -v      # a row and a bar per task
gorun recipes/07-big-upload/Gorunfile -r --plain # plain lines, as in CI
gorun -serve recipes/07-big-upload/Gorunfile       # the web UI: tick the servers and run
```

![The TUI uploading the release to three servers](../../docs/screenshot-upload.png)

The lab's servers are containers on your machine, so uploads are much faster than over a real
network: the bars may finish in a second or two. Use a bigger `SIZE_MB` to watch them for longer.

## What it shows

| Task | Copy | Shows |
|---|---|---|
| `upload-release` | `build/release.tar.gz`, one large file | one bar for the file, then the small checksum file; its verify runs `sha256sum -c` on the server |
| `upload-site` | `build/site`, a folder | one bar for the whole folder, counting every file; its verify counts the files |

## Upload only what changed

Both tasks have `check_copies = true`. Before uploading, gorun compares the MD5 of every file it
would send with the one already on the server. The server hashes its own copy with `md5sum` (or
`md5 -r`, or `openssl`), so only the hashes cross the network, never the 1 GB.

- The first run uploads everything, and each task's `verify` checks it arrived whole.
- Run it again and both tasks show `=` *unchanged*: nothing is sent. The check output lists each
  copy as `same`.
- Change one file of the site (`echo hi >> recipes/07-big-upload/build/site/css/file-0`) and run
  again: `upload-release` stays unchanged, and `upload-site` uploads the folder. Its check output
  (shown with `-v`, and kept in History) says why: `changed ... (1 of 200 files: css/file-0)`.
- `--check` tells you which uploads would be sent without sending anything; `--force` sends
  everything regardless.
- A server with no MD5 tool can't compare, so its files are uploaded every time, as before.

- **Ctrl-C** (or **Stop** in the web UI) stops the uploads part way. Run it again to start over:
  a half-sent file doesn't match, so it is sent again.
- `--plain` prints a line as each large copy starts, about every 10%, and when it is done:

  ```
  server1: upload-release sending release.tar.gz (1.0 GB)
  server1: upload-release sending release.tar.gz 10% (102.4 MB / 1.0 GB · 59.8 MB/s)
  ...
  server1: upload-release sent release.tar.gz (1.0 GB in 17.1s, 59.9 MB/s)
  ```

- `gorun recipes/07-big-upload/Gorunfile -r --task cleanup` removes `/tmp/gorun-upload` from the
  servers.

See [upload progress](../../docs/gorunfile.md#upload-progress) for how it works, and
[`CopyProgressEvent`](../../docs/history.md#copyprogressevent-copy_progress) to show it in your own
program.
