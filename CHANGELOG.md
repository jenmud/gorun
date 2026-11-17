# Changelog

Release notes for each tag are generated from the commit subjects (see `.goreleaser.yaml`), so
the subjects say what changed. This file lists the changes since the last tag, and is copied into
the release notes when a version is cut.

## Unreleased (since v0.30.0)

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
- **?** opens the help, as ctrl+k does. The help lists each vim key next to its plain key, and
  scrolls with j/k when it is taller than the screen. The live view has a help too.
- **Changed:** clearing the picker's selection moves from ctrl+d to **alt+d**, since ctrl+d now
  moves half a page. A single **g** no longer jumps to the top; use **gg** or home.

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
