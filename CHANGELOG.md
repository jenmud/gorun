# Changelog

Release notes for each tag are generated from the commit subjects (see `.goreleaser.yaml`), so
the subjects say what changed. This file lists the changes since the last tag, and is copied into
the release notes when a version is cut.

## Unreleased (since v0.25.0)

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

### Fixes
- `copy` ignored a copy's `mode`, as above.
