# Changelog

Release notes for each tag are generated from the commit subjects (see `.goreleaser.yaml`), so
the subjects say what changed. This file lists the changes since the last tag, and is copied into
the release notes when a version is cut.

## Unreleased (since v0.31.0)

### Web UI: Live and Run are separate pages
- The front page is now **Live**: the stats, recent runs and the dashboard of what is running,
  from the web UI, the terminal or a schedule. Picking servers and tasks moved to its own **Run**
  page at `/run`, with the servers, bundles and tasks side by side. Starting a run goes to Live;
  a run that can't start stays on Run with the reason. "Run this bundle" links open `/run`.
- The live dashboard keeps up with big fleets: task events now update open pages at most four
  times a second, as copy progress already did, instead of once per event. A 42-server run sent
  690 whole-dashboard updates and left a browser about ten seconds behind; it now sends about 50.
  A run still appears, and finishes, at once.

## v0.31.0 (since v0.30.0)

### Vim keys and / to filter, on every screen
- The picker no longer filters as you type. Press **/** to filter, then type; **enter** keeps
  the filter and **esc** clears it. The search box above the servers is gone: the filter shows
  on the counts line. **/** also filters the Groups tab, the history's runs and an opened run's
  servers and tasks, the live view's servers, and the `--list-*` trees.
- Every screen takes vim keys as well as the plain ones: j/k, h/l, gg/G, ctrl+u/ctrl+d for half
  a page, H/L and ctrl+w h/l/w for panes, gt/gT for tabs, and 1 to 5 to jump to a tab. The
  arrows, pgup/pgdn, home/end, tab and shift+tab still work.
- The filter's matches are picked out in every list: server names and addresses, task and
  bundle names and descriptions, run titles, IDs and hosts. The row under the cursor keeps them
  picked out too.
- Selecting works the same everywhere: **alt+s** selects all and **alt+d** deselects all. On the
  servers they select every shown server (alt+s again deselects them); in the tasks pane they tick
  and untick the server's tasks; on Bundles they check and uncheck every bundle shown. The Servers
  tab's help is two lines: the first selects, the second moves.
- Each tab shows its number before its name (`1 Servers │ 2 Tasks │ …`), the key that goes to it.
- **?** opens the help, as ctrl+k does. The help lists each vim key next to its plain key, and
  scrolls with j/k when it is taller than the screen. The live view has a help too.
- **Removed:** `scripts/ansible2gorun.py`, the Ansible converter, with its tests and the
  `scripts/testdata/ansible-demo` example.
- **Changed:** select all shown servers moves from ctrl+a to **alt+s**, clear the selection
  from ctrl+d (which now moves half a page) to **alt+d**, and tick and untick a server's tasks
  from alt+t and ctrl+x to **alt+s** and **alt+d** in the tasks pane.
  A single **g** no longer jumps to the top; use **gg** or home.

## v0.30.0 (since v0.29.0)

### Writing a Gorunfile from Go
- `Config.Encode(w)` and `Config.Marshal()` write a config as a Gorunfile, the inverse of `Load`:
  settings, vars, envvars, ssh, tasks, bundles, server groups, schedules, and a
  `[servers.hosts.<name>]` table per server in `Config.Servers` order. Keys are sorted and empty
  values left out, so the output is stable and minimal; values are written exactly as they are,
  never filled in or expanded. A program that loads a task library and adds servers from its own
  inventory can show, save or diff the Gorunfile, and load the text to run it.
- The TOML tags on `Config`, `Task`, `Copy`, `Bundle`, `ServerGroup`, `Schedule`, `Server` and
  `SSH` gain `omitempty`/`omitzero`. Reading a Gorunfile is unchanged.

## v0.26.0 to v0.29.0 (since v0.25.0)

### Copies: permissions and ownership
- A copy takes `mode` (or `perm`), an octal permission such as `0644`, and `owner` and `group`.
  The mode applies to a copied file, or to each file in a copied folder. Owner and group are set
  with `chown`; with `sudo = true` on the task they run under `sudo -n`.
- `mode` was documented before but ignored, so copied files kept their local permissions. It now
  takes effect. A mode that isn't octal fails when the task is filled in.
- `check_copies` also compares the target's mode, owner and group, so a change to only the
  permissions copies the file again. The output says what differs, such as
  `changed  /etc/app.conf (mode 0640 wanted, rw------- found)`.
- Task summaries in every view show the settings after the destination, such as
  `app.conf → /etc/app.conf (mode 0640, owner nobody, group nogroup)`.
- The web form takes `mode=0640`, `owner=name` and `group=name` after a copy's destination.

### Converting Ansible
- `scripts/ansible2gorun.py` converts an Ansible inventory and playbooks into a Gorunfile. Tasks
  it can't convert are kept and fail on purpose, and are listed in the output. It doesn't handle
  Ansible facts yet. See `scripts/testdata/ansible-demo` for an example it converts in full.

### Search
- The Tasks and Bundles tabs of the TUI filter the tree: press **/**, type, then **enter** to keep
  the filter or **esc** to clear it. A task matches on its name, description, command, copies,
  check and verify.
- The web Tasks and Bundles pages search more of each task or bundle, match every word typed, and
  show how many are shown. **esc** clears the box.

### Fixes
- `copy` ignored a copy's `mode`, as above.
