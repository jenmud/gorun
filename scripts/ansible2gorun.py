#!/usr/bin/env python3
"""ansible2gorun converts an Ansible inventory and playbooks into a Gorunfile.

    scripts/ansible2gorun.py -i inventories/prod ansible/site.yml -o gorun-out
    gorun gorun-out/Gorunfile --list-servers

It writes OUT/Gorunfile, and copies the files the tasks copy or template into OUT/files/.

What it converts:
- inventory: groups, children, host ranges (web[01:03]), group_vars and host_vars, and the
  ansible_user, ansible_port and ansible_ssh_private_key_file settings, which become ssh tables
- plays, roles (defaults, vars, params, dependencies and handlers), include_tasks and
  import_tasks, include_role, blocks, vars_files
- variables: {{ name }} becomes a Gorun template reference to the host's variable
- when: a condition on the converter's variables (== != and or not in) becomes a check that
  skips the task when it is false. It can't see facts (ansible_os_family and the like).
- loop and with_items over a literal list, or over a variable whose value is a literal list
- modules: shell, command, raw (with chdir, creates, removes), apt, service and systemd, file,
  copy (src, content, mode, owner, group), template (rendered with Gorun's templates), set_fact
  (as a host variable), assert, and debug and meta (dropped)

What it can't convert is kept as a task whose command is `false` and whose description says
NOT CONVERTED, so it fails on purpose rather than being skipped. Each case is listed at the top
of the Gorunfile and on stderr. Read that list before you run anything. Typical reasons: facts
(ansible_os_family, ansible_distribution), Jinja filters, registered results used later, loops
over variables that aren't literal lists, and modules such as yum, lineinfile and cron.

Differences you should know about:
- Handlers run on every run. Ansible runs one only when a task notifies it.
- Tags are kept in each task's description; they don't select tasks.
- changed_when with a literal true or false is ignored. Gorun decides "changed" from checks.
- A task skipped by a when shows as "unchanged", not "skipped".
- serial, strategy and become_user are not converted.
- Secrets (ansible_password and friends) and encrypted vault files are never written. Set them
  through the environment instead.
- Variables in the same layer can't see each other in Gorun, so the converter writes their
  values in place. Its own references are resolved, not Gorun's.

Needs PyYAML (pip install pyyaml), which Ansible itself installs. Python 3.11 or newer is needed
for the tests (they read the output with tomllib).
"""

import argparse
import fnmatch
import json
import os
import re
import shlex
import shutil
import sys

try:
    import yaml
except ImportError:  # pragma: no cover - only when PyYAML is missing
    sys.exit("ansible2gorun needs PyYAML: pip install pyyaml")


BARE_KEY = re.compile(r"^[A-Za-z0-9_-]+$")
IDENT = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")
JINJA_VAR = re.compile(r"\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}")
JINJA_ANY = re.compile(r"\{\{|\{%|\{#")

# Ansible connection settings that become a host's ssh table, and their Gorun names.
SSH_KEYS = {"ansible_user": "username", "ansible_port": "port", "ansible_ssh_private_key_file": "identityfile"}
# Settings that must never reach a Gorunfile.
SECRET_KEYS = {"ansible_password", "ansible_ssh_pass", "ansible_ssh_password", "ansible_become_password",
               "ansible_become_pass", "ansible_sudo_pass", "ansible_sudo_password"}
# Task keywords that are not a module.
TASK_KEYS = {"name", "when", "become", "become_user", "become_method", "become_flags", "tags", "register",
             "notify", "loop", "loop_control", "ignore_errors", "ignore_unreachable", "changed_when",
             "failed_when", "environment", "args", "delegate_to", "delegate_facts", "run_once", "no_log",
             "vars", "until", "retries", "delay", "check_mode", "diff", "listen", "throttle",
             "any_errors_fatal", "port", "remote_user", "connection", "timeout", "debugger",
             "module_defaults", "collections", "block", "rescue", "always", "local_action"}
# Modules that do nothing on a server, so their task is dropped.
NOOP_MODULES = {"debug", "setup", "meta", "gather_facts", "ping"}
TRUE = {True, "yes", "true", "on", 1, "1"}


class Unconvertible(Exception):
    """A construct the converter can't translate. Its task is kept, and fails on purpose."""


class Report:
    """Everything the converter couldn't translate or skipped, for the Gorunfile and stderr."""

    def __init__(self):
        self.items = []

    def note(self, where, text):
        if os.path.isabs(where):
            where = os.path.relpath(where)
        self.items.append((where, text))


class Ctx:
    """Where a task is being converted: its play, role and the files it can refer to."""

    def __init__(self, **kw):
        self.__dict__.update(kw)

    def with_(self, **kw):
        d = dict(self.__dict__)
        d.update(kw)
        return Ctx(**d)


def when_list(w):
    """A task's when: one expression, or a list that must all hold."""
    if w is None:
        return []
    if isinstance(w, bool):
        return ["true" if w else "false"]
    if isinstance(w, list):
        return [str(x) for x in w]
    return [str(w)]


def truthy(v):
    if isinstance(v, bool):
        return v
    if isinstance(v, (int, float)):
        return v != 0
    return isinstance(v, str) and v.strip().lower() in ("yes", "true", "on", "1")


def slug(s):
    return re.sub(r"[^a-z0-9]+", "-", str(s).lower()).strip("-") or "task"


def sh(s):
    return shlex.quote(str(s))


def qk(key):
    """A TOML key: bare when it can be, quoted otherwise (host names have dots)."""
    key = str(key)
    return key if BARE_KEY.match(key) else json.dumps(key)


def tv(v):
    """A TOML value."""
    if isinstance(v, bool):
        return "true" if v else "false"
    if isinstance(v, int):
        return str(v)
    if isinstance(v, float):
        return repr(v)
    if isinstance(v, str):
        return json.dumps(v, ensure_ascii=False)
    if isinstance(v, (list, tuple)):
        return "[" + ", ".join(tv(x) for x in v if x is not None) + "]"
    if isinstance(v, dict):
        items = [f"{qk(k)} = {tv(x)}" for k, x in v.items() if x is not None]
        return "{ " + ", ".join(items) + " }" if items else "{}"
    return json.dumps(str(v))


def mode_str(m):
    if m is None:
        return None
    if isinstance(m, int):
        return f"{m:04o}"
    return str(m).strip()


def load_yaml_file(path, rep):
    """A YAML file's data, or None when it is missing, encrypted or not YAML (reported)."""
    if not os.path.isfile(path):
        return None
    if path.endswith((".md", ".txt")):
        return None
    try:
        with open(path, encoding="utf-8") as f:
            text = f.read()
    except UnicodeDecodeError:
        rep.note(path, "not text, not read")
        return None
    if text.startswith("$ANSIBLE_VAULT"):
        rep.note(path, "encrypted with ansible-vault, not converted: put its values in an environment variable")
        return None
    try:
        return yaml.safe_load(text)
    except yaml.YAMLError as e:
        rep.note(path, f"not valid YAML, skipped: {str(e).splitlines()[0]}")
        return None


def load_vars_path(path, rep):
    """Vars from a file, or from every file in a directory (group_vars/<name>/)."""
    out = {}
    if os.path.isdir(path):
        for name in sorted(os.listdir(path)):
            if not name.startswith("."):
                out.update(load_vars_path(os.path.join(path, name), rep))
        return out
    data = load_yaml_file(path, rep)
    if isinstance(data, dict):
        out.update(data)
    return out


def vars_for(bases, kind, name, rep):
    """The vars Ansible reads for a group or host from kind (group_vars or host_vars) in each base."""
    out = {}
    for base in bases:
        for p in (os.path.join(base, kind, name), os.path.join(base, kind, name + ".yml")):
            if os.path.exists(p):
                out.update(load_vars_path(p, rep))
    return out


RANGE = re.compile(r"^(.*?)\[([0-9a-zA-Z]+):([0-9a-zA-Z]+)\](.*)$")


def expand_pattern(pat):
    """web[01:03].example.com -> web01..web03.example.com"""
    m = RANGE.match(pat)
    if not m:
        return [pat]
    pre, a, b, post = m.groups()
    if a.isdigit() and b.isdigit():
        width = len(a) if a.startswith("0") else 1
        return [h for n in range(int(a), int(b) + 1) for h in expand_pattern(f"{pre}{str(n).zfill(width)}{post}")]
    if len(a) == 1 and len(b) == 1:
        return [h for c in map(chr, range(ord(a), ord(b) + 1)) for h in expand_pattern(f"{pre}{c}{post}")]
    return [pat]


class Inventory:
    def __init__(self):
        self.groups = {}     # name -> {"hosts": set, "children": set, "vars": dict}
        self.hostvars = {}   # host -> inline vars from the host line
        self.order = []      # hosts, in the order they appear

    def group(self, name):
        return self.groups.setdefault(name, {"hosts": set(), "children": set(), "vars": {}})


def parse_ini_inventory(paths, inv, rep):
    """Adds the INI inventory files to inv. Each file is read as one list, as Ansible does."""
    for path in paths:
        section, kind = "ungrouped", "hosts"
        with open(path, encoding="utf-8") as f:
            for raw in f:
                try:
                    toks = shlex.split(raw, comments=True)
                except ValueError as e:
                    rep.note(path, f"line skipped: {e}")
                    continue
                if not toks:
                    continue
                if len(toks) == 1 and toks[0].startswith("[") and toks[0].endswith("]"):
                    name = toks[0][1:-1]
                    kind = "hosts"
                    if ":" in name:
                        name, kind = name.split(":", 1)
                    section = name
                    if section != "ungrouped":
                        inv.group(section)
                    continue
                if kind == "vars":
                    k, _, v = " ".join(toks).partition("=")
                    inv.group(section)["vars"][k.strip()] = v.strip()
                elif kind == "children":
                    child = toks[0]
                    inv.group(section)["children"].add(child)
                    inv.group(child)
                else:
                    inline = dict(t.split("=", 1) for t in toks[1:] if "=" in t)
                    for h in expand_pattern(toks[0]):
                        if h not in inv.hostvars:
                            inv.hostvars[h] = {}
                            inv.order.append(h)
                        inv.hostvars[h].update(inline)
                        if section != "ungrouped":
                            inv.group(section)["hosts"].add(h)


class Converter:
    def __init__(self, inv_paths, playbooks, roles_dirs, out_dir):
        self.rep = Report()
        self.out_dir = out_dir
        self.roles_dirs = roles_dirs
        self.inv = Inventory()
        self.tasks = {}          # task key -> spec, in order
        self.host_tasks = {}     # host -> task keys, in run order
        self.extra = {}          # host -> play and role vars, which beat the inventory
        self.raw_extra = {}      # host -> those vars as written, for inlining references
        self.known = set()       # every variable name the converter has seen
        self.shipped = 0
        self.item = None         # the loop item being converted, written into the task
        self._loop_targets = []  # the hosts the playbook's current play runs on
        self.playbooks = playbooks
        self.inv_paths = inv_paths
        self.bases = []
        self.count = {"plays": 0, "roles": 0, "tasks": 0, "unconverted": 0}

        for p in inv_paths:
            base = p if os.path.isdir(p) else os.path.dirname(os.path.abspath(p))
            self.bases.append(base)
        for pb in playbooks:
            self.bases.append(os.path.dirname(os.path.abspath(pb)))
        self.bases = list(dict.fromkeys(self.bases))

    # ---- inventory -------------------------------------------------------------------------

    def load_inventory(self):
        files = []
        for p in self.inv_paths:
            if os.path.isdir(p):
                files += [os.path.join(p, n) for n in sorted(os.listdir(p))
                          if os.path.isfile(os.path.join(p, n)) and not n.startswith(".")]
            else:
                files.append(p)
        parse_ini_inventory(files, self.inv, self.rep)
        self.inv.group("ungrouped")

        self.gv = {"all": vars_for(self.bases, "group_vars", "all", self.rep)}
        for name, g in self.inv.groups.items():
            if name == "ungrouped":
                continue
            merged = dict(g["vars"])
            merged.update(vars_for(self.bases, "group_vars", name, self.rep))
            self.gv[name] = merged
        self.hv_file = {h: vars_for(self.bases, "host_vars", h, self.rep) for h in self.inv.order}

        self.parents = {}
        for name, g in self.inv.groups.items():
            for c in g["children"]:
                self.parents.setdefault(c, set()).add(name)
        self.memberships = {h: set() for h in self.inv.order}
        for name, g in self.inv.groups.items():
            if name == "ungrouped":
                continue
            for h in g["hosts"]:
                self.memberships[h].add(name)
                self.memberships[h] |= self.ancestors(name)

        for g in self.gv.values():
            self.known |= {k for k in g if IDENT.match(k)}
        # a group named in group_vars but not in the inventory has no hosts, so its vars do nothing
        for v in self.hv_file.values():
            self.known |= {k for k in v if IDENT.match(k)}
        for v in self.inv.hostvars.values():
            self.known |= {k for k in v if IDENT.match(k)}

    def ancestors(self, name, seen=frozenset()):
        out = set()
        for p in self.parents.get(name, ()):
            if p not in seen:
                out |= {p} | self.ancestors(p, seen | {name})
        return out

    def depth(self, name, seen=frozenset()):
        ps = [p for p in self.parents.get(name, ()) if p not in seen]
        return 0 if not ps else 1 + max(self.depth(p, seen | {name}) for p in ps)

    def host_groups(self, h):
        """The groups a host is in, parents first: later groups win, as Ansible merges them."""
        return sorted(self.memberships.get(h, ()), key=lambda g: (self.depth(g), g))

    def add_localhost(self):
        if "localhost" not in self.inv.hostvars:
            self.inv.hostvars["localhost"] = {"ansible_connection": "local"}
            self.inv.order.append("localhost")
            self.hv_file["localhost"] = {}
            self.memberships["localhost"] = set()

    def flat(self, h):
        """Every inventory setting for a host, with the highest-precedence value winning."""
        out = dict(self.gv.get("all", {}))
        for g in self.host_groups(h):
            out.update(self.gv.get(g, {}))
        out.update(self.inv.hostvars.get(h, {}))
        out.update(self.hv_file.get(h, {}))
        return out

    def is_local(self, h):
        return str(self.flat(h).get("ansible_connection", "")) == "local"

    def inv_names(self, h):
        return set(self.flat(h))

    def match_term(self, term, where):
        if term == "all":
            return set(self.inv.order)
        if term in self.inv.groups:
            return {h for h in self.inv.order if term in self.memberships.get(h, ())}
        if term == "localhost":
            self.add_localhost()
            return {"localhost"}
        hits = set(fnmatch.filter(self.inv.order, term))
        if not hits:
            self.rep.note(where, f"hosts: {term!r} matches no host in the inventory")
        return hits

    def resolve(self, pattern, where):
        """Ansible's host pattern: group names, host names and globs, joined with : (or ,).
        "!" removes hosts and "&" keeps only hosts that match."""
        result = None
        for term in re.split(r"[:,]", pattern):
            if not term:
                continue
            op = "+"
            if term[0] in "!&":
                op, term = term[0], term[1:]
            matched = self.match_term(term, where)
            if result is None:
                result = matched if op == "+" else set()
            elif op == "+":
                result |= matched
            elif op == "&":
                result &= matched
            else:
                result -= matched
        return [h for h in self.inv.order if result and h in result]

    # ---- variables -------------------------------------------------------------------------

    def literal_list(self, name, item):
        """The value of a variable that is a list of plain values for every host it is used on."""
        found = None
        for h in self._loop_targets:
            value = self.extra.get(h, {}).get(name, self.flat(h).get(name))
            if value is None:
                raise Unconvertible(f"the loop variable {name} is not set in the inventory")
            if found is not None and found != value:
                raise Unconvertible(f"the loop variable {name} differs between hosts")
            found = value
        if found is None:
            raise Unconvertible(f"the loop variable {name} is not set")
        return found

    def scope(self):
        s = {n: f'index .Server.Vars "{n}"' for n in self.known}
        if self.item is not None:
            s["item"] = json.dumps(str(self.item))
        s["inventory_hostname"] = ".Name"
        s["ansible_managed"] = '"Managed by gorun (converted from Ansible)"'
        return s

    def conv(self, value):
        """Ansible's Jinja in a value, as Gorun's Go template. Raises Unconvertible for the rest."""
        if isinstance(value, str):
            return conv_text(value, self.scope())
        if isinstance(value, dict):
            return {k: self.conv(v) for k, v in value.items()}
        if isinstance(value, list):
            return [self.conv(v) for v in value]
        return value

    def set_vars(self, targets, mapping, where, override=True, skip=None):
        """Sets vars on each target host, converting their values. A value that can't be converted
        is dropped and reported; it doesn't stop the rest."""
        for k, v in (mapping or {}).items():
            k = str(k)
            if not IDENT.match(k):
                self.rep.note(where, f"var {k!r} is not a valid name, skipped")
                continue
            if k.startswith("ansible_") or k in SECRET_KEYS:
                continue
            self.known.add(k)
            for h in targets:
                if skip and k in skip(h):
                    continue
                if not override and k in self.extra.setdefault(h, {}):
                    continue
                layer = {**self.extra.get(h, {}), **self.raw_extra.setdefault(h, {}), k: v}
                try:
                    val = self.conv(self.inline_refs(v, layer))
                except Unconvertible as e:
                    self.rep.note(where, f"var {k} not converted: {e}")
                    break
                self.extra[h][k] = val
                self.raw_extra[h][k] = v

    # ---- files -----------------------------------------------------------------------------

    def find_file(self, ctx, rel, sub):
        if os.path.isabs(rel) and os.path.exists(rel):
            return rel
        cands = []
        if ctx.role_dir:
            cands.append(os.path.join(ctx.role_dir, sub, rel))
        cands.append(os.path.join(ctx.playbook_dir, sub, rel))
        cands.append(os.path.join(ctx.base_dir, rel))
        cands.append(os.path.join(ctx.playbook_dir, rel))
        for c in cands:
            if os.path.exists(c):
                return c
        raise Unconvertible(f"{sub[:-1]} {rel} not found")

    def ship(self, path, name=None):
        """Copies a file or directory into OUT/files/ and returns its path relative to the Gorunfile."""
        self.shipped += 1
        base = f"{self.shipped:03d}-{name or os.path.basename(path.rstrip('/'))}"
        dest = os.path.join(self.out_dir, "files", base)
        os.makedirs(os.path.dirname(dest), exist_ok=True)
        if os.path.isdir(path):
            shutil.copytree(path, dest, dirs_exist_ok=True)
        else:
            shutil.copy2(path, dest)
        return f"files/{base}"

    def ship_text(self, text, name):
        self.shipped += 1
        base = f"{self.shipped:03d}-{name}"
        dest = os.path.join(self.out_dir, "files", base)
        os.makedirs(os.path.dirname(dest), exist_ok=True)
        with open(dest, "w", encoding="utf-8") as f:
            f.write(text)
        return f"files/{base}"

    # ---- tasks -----------------------------------------------------------------------------

    def placeholder(self, ctx, what, reason):
        self.count["unconverted"] += 1
        spec = {"description": f"NOT CONVERTED ({what}): {reason}", "cmd": "false"}
        if ctx.local:
            spec["local"] = True
        key = self.new_key(ctx, what)
        self.tasks[key] = spec
        return key

    def new_key(self, ctx, label):
        base = f"{ctx.prefix}-{slug(label)}"
        key, n = base, 2
        while key in self.tasks:
            key, n = f"{base}-{n}", n + 1
        return key

    def convert_tasks(self, items, ctx):
        seq = []
        if items is None:
            return seq
        if not isinstance(items, list):
            self.rep.note(ctx.where, "tasks is not a list, skipped")
            return seq
        for item in items:
            if not isinstance(item, dict):
                self.rep.note(ctx.where, "an item that is not a task was skipped")
                continue
            # ansible.builtin.copy and copy are the same module, so the prefix is dropped
            item = {re.sub(r"^ansible\.(builtin|legacy)\.", "", str(k)): v for k, v in item.items()}
            label = str(item.get("name") or self.module_name(item) or "task")
            if "block" in item:
                if item.get("rescue") or item.get("always"):
                    seq.append(self.placeholder(ctx, label, "rescue and always on a block are not converted yet"))
                    continue
                seq += self.convert_tasks(item["block"], ctx.with_(
                    become=bool(item.get("become", ctx.become)), when=ctx.when + tuple(when_list(item.get("when")))))
                continue
            inc = next((k for k in ("import_tasks", "include_tasks") if k in item), None)
            if inc:
                seq += self.include_file(item, inc, label, ctx)
                continue
            rinc = next((k for k in ("import_role", "include_role") if k in item), None)
            if rinc:
                args = item[rinc] or {}
                params = {k: v for k, v in args.items() if k not in ("name", "tasks_from", "defaults_from", "vars_from", "handlers_from", "allow_duplicates")}
                rctx = ctx.with_(when=ctx.when + tuple(when_list(item.get("when"))))
                seq += self.run_role(str(args.get("name", "")), params, rctx, ())
                continue
            try:
                values = self.loop_values(item)
            except Unconvertible as e:
                self.rep.note(ctx.where, f"task {label!r} not converted: {e}")
                seq.append(self.placeholder(ctx, label, str(e)))
                continue
            if values is None:
                values = [None]
            plain = {k: v for k, v in item.items() if k not in ("loop", "loop_control") and not str(k).startswith("with_")}
            for v in values:
                shown = label if v is None else f"{label} {v}"
                self.item = v
                try:
                    spec = self.task_spec(plain, ctx)
                except Unconvertible as e:
                    self.rep.note(ctx.where, f"task {shown!r} not converted: {e}")
                    seq.append(self.placeholder(ctx, shown, str(e)))
                    continue
                except Exception as e:  # a construct the converter didn't expect: keep the task, failing
                    self.rep.note(ctx.where, f"task {shown!r} not converted: converter error ({type(e).__name__}: {e})")
                    seq.append(self.placeholder(ctx, shown, f"converter error: {type(e).__name__}"))
                    continue
                finally:
                    self.item = None
                if spec is None:
                    continue
                key = self.new_key(ctx, shown)
                self.tasks[key] = spec
                seq.append(key)
                self.count["tasks"] += 1
        return seq

    def loop_values(self, item):
        """The literal items of a task's loop, or None when it has no loop. A loop over a variable
        or over dicts is not converted."""
        src = None
        if "loop" in item:
            src = item["loop"]
        else:
            for k in item:
                if str(k) == "with_items":
                    src = item[k]
                elif str(k).startswith("with_"):
                    raise Unconvertible(f"{k} is not converted yet (only literal lists are)")
        if src is None:
            return None
        if isinstance(src, str):
            m = re.fullmatch(r"\s*\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}\s*", src)
            if not m:
                raise Unconvertible("the loop is not a literal list of values")
            src = self.literal_list(m.group(1), item)
        if not isinstance(src, list) or not all(isinstance(x, (str, int, float)) and not isinstance(x, bool) for x in src):
            raise Unconvertible("the loop is not a literal list of values")
        return src

    def include_file(self, item, kw, label, ctx):
        if "vars" in item:
            return [self.placeholder(ctx, label, "vars on an include are not converted yet")]
        target = item[kw]
        rel = target.get("file") if isinstance(target, dict) else target
        if not rel:
            return [self.placeholder(ctx, label, "include has no file")]
        path = os.path.join(ctx.base_dir, str(rel))
        data = load_yaml_file(path, self.rep)
        if not isinstance(data, list):
            return [self.placeholder(ctx, label, f"{rel} not found or not a task list")]
        return self.convert_tasks(data, ctx.with_(base_dir=os.path.dirname(path),
                                                  when=ctx.when + tuple(when_list(item.get("when")))))

    def module_name(self, item):
        mod = self.find_module(item)
        return mod[0] if mod else None

    def find_module(self, item):
        for k in item:
            if k in TASK_KEYS or str(k).startswith("with_"):
                continue
            return k, item[k]
        return None

    def task_spec(self, item, ctx):
        for k in ("failed_when", "until", "delegate_to", "run_once", "throttle", "local_action"):
            if k in item:
                raise Unconvertible(f"{k} is not converted yet")
        cw = item.get("changed_when")
        if "changed_when" in item and not isinstance(cw, bool) and str(cw).strip().lower() not in ("yes", "no", "true", "false"):
            raise Unconvertible("changed_when with a test is not converted yet")
        if "args" in item:
            if not isinstance(item["args"], dict):
                raise Unconvertible("args that is not a mapping is not converted yet")
            found = self.find_module(item)
            if found and found[0] in ("shell", "command", "raw", "ansible.builtin.shell", "ansible.builtin.command"):
                base = found[1]
                base = {"cmd": base} if isinstance(base, str) else dict(base or {})
                item = {**{k: v for k, v in item.items() if k != "args"}, found[0]: {**base, **item["args"]}}
            else:
                raise Unconvertible("args is only converted on shell and command tasks")
        if any(str(k).startswith("with_") for k in item):
            raise Unconvertible("loops (with_*) are not converted yet")
        found = self.find_module(item)
        if not found:
            raise Unconvertible("no module")
        mod, args = found
        short = mod
        for prefix in ("ansible.builtin.", "ansible.legacy."):
            if short.startswith(prefix):
                short = short[len(prefix):]
        if short in NOOP_MODULES:
            return None
        if str(item.get("become_user", "root")) != "root":
            raise Unconvertible("become_user other than root is not converted yet")
        conds = list(ctx.when) + when_list(item.get("when"))
        if short == "set_fact":
            if conds:
                raise Unconvertible("set_fact with a when is not converted yet")
            self.set_vars(ctx.targets, args if isinstance(args, dict) else {}, ctx.where, override=True)
            self.rep.note(ctx.where, f"set_fact {', '.join(map(str, (args or {}).keys()))} is set as a host var: right for static values only")
            return None
        if short == "assert":
            if conds:
                raise Unconvertible("assert with a when is not converted yet")
            tests = when_list((args or {}).get("that"))
            if not tests:
                raise Unconvertible("assert without a test")
            cond = " ".join(f"({when_to_go(t, self.scope())})" for t in tests)
            cond = cond if len(tests) == 1 else "and " + cond
            return {"cmd": "{{ if " + cond + " }}true{{ else }}false{{ end }}",
                    "description": (str(item.get("name") or "assert")).replace("{{", "{ {")}
        spec = self.module(short, args, ctx)
        if not spec.get("cmd"):
            raise Unconvertible("no command")
        if conds:
            if spec.get("check_copies"):
                raise Unconvertible("when on a copy is not converted yet")
            parts = [when_to_go(c, self.scope()) for c in conds]
            cond = " ".join(f"({p})" for p in parts) if len(parts) == 1 else "and " + " ".join(f"({p})" for p in parts)
            inner = spec.get("check") or "false"
            # a false condition makes the check pass, so the task is skipped as unchanged
            spec["check"] = "{{ if " + cond + " }}" + inner + "{{ else }}true{{ end }}"
        name = str(item.get("name") or "")
        tags = item.get("tags")
        desc = name or short
        if tags:
            desc += f" (tags: {', '.join(map(str, tags)) if isinstance(tags, list) else tags})"
        spec["description"] = desc.replace("{{", "{ {")
        if truthy(item.get("become", ctx.become)):
            spec["sudo"] = True
        if item.get("ignore_errors") is True:
            spec["cmd"] = f"({spec['cmd']}) || true"
        if isinstance(item.get("environment"), dict):
            spec["envvars"] = {str(k): str(self.conv(v)) for k, v in item["environment"].items()}
        if ctx.local:
            spec["local"] = True
        return spec

    def module(self, mod, args, ctx):
        c = self.conv
        if mod in ("shell", "command", "raw"):
            if isinstance(args, dict):
                extra = set(args) - {"cmd", "chdir", "creates", "removes", "executable"}
                if extra:
                    raise Unconvertible(f"{mod} options {sorted(extra)} are not converted yet")
                cmd = str(args.get("cmd", ""))
            else:
                cmd = str(args or "")
            if not cmd:
                raise Unconvertible("empty command")
            cmd = c(cmd)
            spec = {}
            if isinstance(args, dict):
                if "chdir" in args:
                    cmd = f"cd {sh(c(str(args['chdir'])))} && ({cmd})"
                if "creates" in args:
                    spec["check"] = f"test -e {sh(c(str(args['creates'])))}"
                if "removes" in args:
                    spec["check"] = f"test ! -e {sh(c(str(args['removes'])))}"
            spec["cmd"] = cmd
            return spec

        if mod == "apt":
            if isinstance(args, str):
                args = dict(t.split("=", 1) for t in shlex.split(args) if "=" in t)
            if not isinstance(args, dict):
                raise Unconvertible("apt needs options")
            extra = set(args) - {"name", "pkg", "state", "update_cache", "cache_valid_time", "install_recommends",
                                 "lock_timeout"}
            if extra:
                raise Unconvertible(f"apt options {sorted(extra)} are not converted yet")
            names = args.get("name") or args.get("pkg") or []
            if isinstance(names, str):
                names = [n.strip() for n in names.split(",") if n.strip()]
            names = [str(c(str(n))) for n in names]
            state = str(args.get("state", "present"))
            update = truthy(args.get("update_cache", False))
            recs = "" if truthy(args.get("install_recommends", True)) else " --no-install-recommends"
            env = "DEBIAN_FRONTEND=noninteractive "
            cmds, check = [], None
            if update:
                cmds.append("apt-get update")
            if names:
                pk = " ".join(sh(n) for n in names)
                if state in ("present", "installed"):
                    cmds.append(f"{env}apt-get install -y{recs} {pk}")
                    check = f"dpkg -s {pk}" if not update else None
                elif state == "latest":
                    cmds.append(f"{env}apt-get install -y{recs} {pk}")
                elif state in ("absent", "removed"):
                    cmds.append(f"{env}apt-get remove -y {pk}")
                else:
                    raise Unconvertible(f"apt state {state} is not converted yet")
            spec = {"cmd": " && ".join(cmds) or "true"}
            if check:
                spec["check"] = check
            return spec

        if mod in ("service", "systemd", "systemd_service"):
            if not isinstance(args, dict) or not args.get("name"):
                raise Unconvertible(f"{mod} needs a name")
            name = sh(c(str(args["name"])))
            state = args.get("state")
            enabled = args.get("enabled")
            daemon = truthy(args.get("daemon_reload", False))
            if state is None and enabled is None and not daemon:
                raise Unconvertible(f"{mod} needs a state or enabled")
            cmds, checks = [], []
            if daemon:
                cmds.append("systemctl daemon-reload")
            if enabled is not None:
                on = truthy(enabled)
                cmds.append(f"systemctl {'enable' if on else 'disable'} {name}")
                checks.append(f"systemctl is-enabled --quiet {name}" if on else f"! systemctl is-enabled --quiet {name}")
            verb = {"started": "start", "stopped": "stop", "restarted": "restart", "reloaded": "reload"}.get(str(state))
            if state is not None and verb is None:
                raise Unconvertible(f"{mod} state {state} is not converted yet")
            if verb:
                cmds.append(f"systemctl {verb} {name}")
                if state == "started":
                    checks.append(f"systemctl is-active --quiet {name}")
                if state == "stopped":
                    checks.append(f"! systemctl is-active --quiet {name}")
            spec = {"cmd": " && ".join(cmds)}
            if checks and verb not in ("restart", "reload"):
                spec["check"] = " && ".join(checks)
            return spec

        if mod == "file":
            if not isinstance(args, dict):
                raise Unconvertible("file needs options")
            path = str(c(str(args.get("path") or args.get("dest") or args.get("name") or "")))
            if not path:
                raise Unconvertible("file needs a path")
            state = str(args.get("state", "file"))
            mode = mode_str(args.get("mode"))
            owner, group = args.get("owner"), args.get("group")
            cmds, check = [], None
            if state == "directory":
                cmds.append(f"mkdir -p {sh(path)}")
                check = f"test -d {sh(path)}"
            elif state == "absent":
                cmds.append(f"rm -rf {sh(path)}")
                check = f"test ! -e {sh(path)}"
            else:
                raise Unconvertible(f"file state {state} is not converted yet")
            if mode:
                cmds.append(f"chmod {sh(c(mode))} {sh(path)}")
            if owner or group:
                cmds.append(f"chown {sh(c(str(owner or '')) + ':' + c(str(group or '')))} {sh(path)}")
            spec = {"cmd": " && ".join(cmds)}
            if check and not (mode or owner or group):
                spec["check"] = check
            return spec

        if mod in ("copy", "template"):
            return self.copy_module(mod, args, ctx)

        raise Unconvertible(f"module {mod} is not converted yet")

    def copy_module(self, mod, args, ctx):
        if not isinstance(args, dict):
            raise Unconvertible(f"{mod} needs options")
        dst = str(self.conv(str(args.get("dest") or args.get("dst") or "")))
        if not dst:
            raise Unconvertible(f"{mod} needs a dest")
        mode = mode_str(args.get("mode"))
        if mode and not re.fullmatch(r"[0-7]{3,4}", mode):
            raise Unconvertible(f"symbolic mode {mode} is not converted yet")
        owner, group = args.get("owner"), args.get("group")
        entry = {"dst": dst}
        if mode:
            entry["mode"] = mode
        if mod == "copy":
            if "content" in args:
                entry["content"] = self.conv(str(args["content"]))
            elif "src" in args:
                src = str(args["src"])
                if src.endswith("/"):
                    entry = {"dir": dst, "src": self.ship(self.find_file(ctx, src.rstrip("/"), "files")),
                             **({"mode": mode} if mode else {})}
                else:
                    entry["src"] = self.ship(self.find_file(ctx, str(self.conv(src)), "files"))
            else:
                raise Unconvertible("copy needs src or content")
        else:
            src = str(args.get("src", ""))
            if not src:
                raise Unconvertible("template needs a src")
            path = self.find_file(ctx, str(self.conv(src)), "templates")
            with open(path, encoding="utf-8") as f:
                text = f.read()
            name = os.path.basename(path)
            if name.endswith(".j2"):
                name = name[:-3]
            entry["src"] = self.ship_text(conv_text(text, self.scope()), name)
            entry["render"] = True
        cmds = []
        if owner or group:
            cmds.append(f"chown {sh(str(owner or '') + ':' + str(group or ''))} {sh(dst)}")
        spec = {"cmd": " && ".join(cmds) or "true", "copy": [entry], "check_copies": True}
        return spec

    # ---- plays and roles -------------------------------------------------------------------

    def find_role(self, name, ctx):
        # a role name can be a path from the playbook's directory, as in custom_roles/users
        for root in self.roles_dirs + self.bases:
            cand = os.path.join(root, name)
            if os.path.isdir(cand) and any(os.path.isdir(os.path.join(cand, d)) for d in ("tasks", "defaults", "meta")):
                return cand
        return None

    def run_role(self, name, params, ctx, stack):
        if not name:
            return [self.placeholder(ctx, "role", "role without a name")]
        if name in stack:
            self.rep.note(ctx.where, f"role {name} depends on itself, skipped")
            return []
        rdir = self.find_role(name, ctx)
        if rdir is None:
            return [self.placeholder(ctx, f"role {name}", "not found in roles_path (collection roles are not converted)")]
        self.count["roles"] += 1
        meta = load_yaml_file(os.path.join(rdir, "meta", "main.yml"), self.rep) or {}
        seq = []
        for dep in (meta.get("dependencies") or []) if isinstance(meta, dict) else []:
            if isinstance(dep, str):
                dname, dparams = dep, {}
            elif isinstance(dep, dict):
                dname = dep.get("role") or dep.get("name")
                dparams = {k: v for k, v in dep.items() if k not in ("role", "name", "tags", "when")}
            else:
                continue
            dctx = ctx.with_(when=ctx.when + tuple(when_list(dep.get("when") if isinstance(dep, dict) else None)))
            seq += self.run_role(str(dname), dparams, dctx, stack + (name,))

        defaults = load_yaml_file(os.path.join(rdir, "defaults", "main.yml"), self.rep) or {}
        rvars = load_yaml_file(os.path.join(rdir, "vars", "main.yml"), self.rep) or {}
        where = f"{ctx.where}: role {name}"
        # defaults lose to every inventory setting; role vars and params beat the inventory
        for h in ctx.targets:
            inv = self.inv_names(h)
            self.set_vars([h], {k: v for k, v in defaults.items() if k not in inv and k not in self.extra.get(h, {})},
                          where, override=False)
        self.set_vars(ctx.targets, rvars, where, override=True)
        self.set_vars(ctx.targets, params, where, override=True)

        rctx = ctx.with_(role_dir=rdir, base_dir=os.path.join(rdir, "tasks"), where=where)
        seq += self.convert_tasks(load_yaml_file(os.path.join(rdir, "tasks", "main.yml"), self.rep) or [], rctx)
        handlers = load_yaml_file(os.path.join(rdir, "handlers", "main.yml"), self.rep) or []
        if isinstance(handlers, list):
            ctx.handler_items.extend(handlers)
        return seq

    def convert_playbook(self, path):
        pdir = os.path.dirname(os.path.abspath(path))
        fname = os.path.basename(path)
        plays = load_yaml_file(path, self.rep)
        if not isinstance(plays, list):
            self.rep.note(fname, "not a list of plays, skipped")
            return
        for i, play in enumerate(plays, 1):
            if not isinstance(play, dict):
                continue
            if "import_playbook" in play:
                self.rep.note(fname, f"import_playbook {play['import_playbook']} is not converted yet")
                continue
            name = str(play.get("name") or f"play {i}")
            where = f"{fname}: play '{name}'"
            targets = self.resolve(str(play.get("hosts", "all")), where)
            if not targets:
                self.rep.note(where, "matches no host, skipped")
                continue
            self.count["plays"] += 1
            for k in ("serial", "strategy", "max_fail_percentage"):
                if k in play:
                    self.rep.note(where, f"{k} is not converted (Gorun runs hosts in parallel up to max_parallel)")
            for h in targets:
                self.host_tasks.setdefault(h, [])
                self.extra.setdefault(h, {})
            pvars = {}
            for vf in play.get("vars_files") or []:
                if isinstance(vf, str):
                    pvars.update(load_vars_path(os.path.join(pdir, vf), self.rep))
            pvars.update(play.get("vars") or {})
            self.set_vars(targets, pvars, where, override=True)

            local = [self.is_local(h) for h in targets]
            if any(local) and not all(local):
                self.rep.note(where, "hosts mix local and SSH connections, so its tasks run over SSH")
            self._loop_targets = targets
            ctx = Ctx(prefix=slug(name), playbook_dir=pdir, base_dir=pdir, role_dir=None, targets=targets,
                      become=truthy(play.get("become", False)), local=all(local), where=where,
                      handler_items=[], when=())
            ctx = ctx.with_(when=())
            seq = self.convert_tasks(play.get("pre_tasks"), ctx)
            for entry in play.get("roles") or []:
                if isinstance(entry, str):
                    seq += self.run_role(entry, {}, ctx, ())
                elif isinstance(entry, dict):
                    rname = entry.get("role") or entry.get("name")
                    params = {k: v for k, v in entry.items() if k not in ("role", "name", "tags", "become", "become_user", "when")}
                    rctx = ctx.with_(when=tuple(when_list(entry.get("when"))))
                    seq += self.run_role(str(rname), params, rctx, ())
            seq += self.convert_tasks(play.get("tasks"), ctx)
            seq += self.convert_tasks(play.get("post_tasks"), ctx)
            handler_items = list(play.get("handlers") or []) + ctx.handler_items
            if handler_items:
                self.rep.note(where, "handlers run on every run here; Ansible runs one only when a task notifies it")
                seq += self.convert_tasks(handler_items, ctx.with_(prefix=f"{ctx.prefix}-handler"))
            for h in targets:
                self.host_tasks[h] += seq

    def convert(self):
        self.load_inventory()
        for h in self.inv.order:
            self.host_tasks.setdefault(h, [])
            self.extra.setdefault(h, {})
        for pb in self.playbooks:
            self.convert_playbook(pb)

    # ---- output ----------------------------------------------------------------------------

    def group_vars(self, g):
        out = {}
        raw = self.gv.get(g, {})
        for k, v in raw.items():
            if k.startswith("ansible_") or k in SECRET_KEYS or not IDENT.match(k):
                continue
            try:
                out[k] = self.conv(self.inline_refs(v, raw))
            except Unconvertible as e:
                self.rep.note(f"group {g}", f"var {k} not converted: {e}")
        return out

    def host_inventory_vars(self, h):
        """A host's own vars from the inventory: host_vars files beat the settings on the host line."""
        out = {}
        raw = {**self.inv.hostvars.get(h, {}), **self.hv_file.get(h, {})}
        for k, v in raw.items():
            if k.startswith("ansible_") or k in SECRET_KEYS or not IDENT.match(k):
                continue
            try:
                out[k] = self.conv(self.inline_refs(v, raw))
            except Unconvertible as e:
                self.rep.note(f"host {h}", f"var {k} not converted: {e}")
        return out

    def inline_refs(self, value, layer, depth=0):
        """Replaces {{ name }} with the value of name in the same layer. Gorun's values in one layer
        can't see each other, and Ansible's can, so the converter does that part itself."""
        if depth > 10:
            return value
        if isinstance(value, str):
            def sub(m):
                name = m.group(1)
                if name in layer and isinstance(layer[name], (str, int, float)) and not isinstance(layer[name], bool):
                    return self.inline_refs(str(layer[name]), layer, depth + 1)
                return m.group(0)
            return JINJA_VAR.sub(sub, value)
        if isinstance(value, dict):
            return {k: self.inline_refs(v, layer, depth) for k, v in value.items()}
        if isinstance(value, list):
            return [self.inline_refs(v, layer, depth) for v in value]
        return value

    def host_ssh(self, h):
        """The SSH settings for a host, from every layer Ansible reads, the most specific winning."""
        layers = [self.gv.get("all", {})] + [self.gv.get(g, {}) for g in self.host_groups(h)]
        layers += [self.inv.hostvars.get(h, {}), self.hv_file.get(h, {})]
        ssh, secrets = {}, False
        for layer in layers:
            for k, v in layer.items():
                if k in SECRET_KEYS:
                    secrets = True
                if k in SSH_KEYS:
                    ssh[SSH_KEYS[k]] = v
        if secrets:
            self.rep.note(f"host {h}", "a password is set in the inventory and was not written: set it through an environment variable")
        if "port" in ssh:
            try:
                ssh["port"] = int(ssh["port"])
            except (TypeError, ValueError):
                self.rep.note(f"host {h}", f"ansible_port {ssh['port']!r} is not a number, using 22")
                ssh["port"] = 22
        ssh.setdefault("port", 22)
        for k in ("username", "identityfile"):
            if k in ssh:
                ssh[k] = str(ssh[k])
        return ssh

    def write(self, title):
        os.makedirs(self.out_dir, exist_ok=True)
        groups = sorted(g for g in self.inv.groups if g != "ungrouped" and any(g in self.memberships.get(h, ()) for h in self.inv.order))
        out = []
        out.append("# Converted from Ansible by scripts/ansible2gorun.py. Review it before you run it.")
        out.append(f"# Playbooks: {', '.join(os.path.basename(p) for p in self.playbooks)}")
        out.append(f"# Inventory: {', '.join(self.inv_paths)}")
        out.append("#")
        if self.rep.items:
            out.append(f"# {len(self.rep.items)} things need a look (converted tasks that fail on purpose are marked NOT CONVERTED):")
            for where, text in self.rep.items:
                out.append(f"#   - {where}: {text}".replace("\n", " "))
        else:
            out.append("# Nothing was left out.")
        out.append("")
        out.append(f"title = {tv(title)}")
        glob_vars = self.group_vars("all")
        if glob_vars:
            out.append(f"vars = {tv(glob_vars)}")
        out.append("")
        for g in groups:
            out.append(f"[server_groups.{qk(g)}]")
            gvars = self.group_vars(g)
            if gvars:
                out.append(f"vars = {tv(gvars)}")
            out.append("")
        for key, spec in self.tasks.items():
            out.append(f"[tasks.{qk(key)}]")
            for k in ("description", "cmd", "check", "check_copies", "sudo", "local", "envvars"):
                if k in spec and spec[k] not in (None, False, "", {}):
                    out.append(f"{k} = {tv(spec[k])}")
            for entry in spec.get("copy", []):
                out.append(f"[[tasks.{qk(key)}.copy]]")
                out += [f"{k} = {tv(v)}" for k, v in entry.items()]
            out.append("")
        for h in self.inv.order:
            if h == "ungrouped":
                continue
            local = self.is_local(h)
            hgroups = [g for g in self.host_groups(h) if g in groups]
            hvars = self.host_inventory_vars(h)
            hvars.update(self.extra.get(h, {}))
            out.append(f"[servers.hosts.{qk(h)}]")
            hostname = self.flat(h).get("ansible_host", h)
            out.append(f"hostname = {tv(str(self.conv(str(hostname))))}")
            if hgroups:
                out.append(f"groups = {tv(hgroups)}")
            out.append(f"tasks = {tv(self.host_tasks.get(h, []))}")
            if hvars:
                out.append(f"vars = {tv(hvars)}")
            if not local:
                out.append("")
                out.append(f"[servers.hosts.{qk(h)}.ssh]")
                out += [f"{k} = {tv(v)}" for k, v in self.host_ssh(h).items()]
            out.append("")
        os.makedirs(self.out_dir, exist_ok=True)
        path = os.path.join(self.out_dir, "Gorunfile")
        with open(path, "w", encoding="utf-8") as f:
            f.write("\n".join(out))
        return path


WHEN_TOKEN = re.compile(r"""\s*(?:(?P<str>"[^"]*"|'[^']*')|(?P<num>-?\d+(?:\.\d+)?)|(?P<op>==|!=|\(|\)|\[|\]|,)|(?P<word>[A-Za-z_][A-Za-z0-9_]*))""")


def when_to_go(text, scope):
    """An Ansible when expression as a Go template condition. It knows the variables the converter
    knows, and the operators == != and or not, with parentheses. Anything else raises Unconvertible."""
    text = str(text).strip()
    if text.startswith("{{") and text.endswith("}}"):
        text = text[2:-2].strip()
    toks, pos = [], 0
    while pos < len(text):
        if text[pos:].strip() == "":
            break
        m = WHEN_TOKEN.match(text, pos)
        if not m or m.end() == pos:
            raise Unconvertible(f"when {text.strip()[:60]!r}: syntax the converter doesn't read")
        pos = m.end()
        kind = m.lastgroup
        toks.append((kind, m.group(kind)))

    i = 0

    def peek():
        return toks[i] if i < len(toks) else (None, None)

    def take():
        nonlocal i
        t = peek()
        i += 1
        return t

    def or_():
        parts = [and_()]
        while peek() == ("word", "or"):
            take()
            parts.append(and_())
        return parts[0][1] if len(parts) == 1 else "or " + " ".join(f"({p[1]})" for p in parts)

    def and_():
        parts = [not_()]
        while peek() == ("word", "and"):
            take()
            parts.append(not_())
        return parts[0] if len(parts) == 1 else ("expr", "and " + " ".join(f"({p[1]})" for p in parts))

    def not_():
        if peek() == ("word", "not"):
            take()
            return ("expr", f"not ({not_()[1]})")
        return cmp_()

    def atom():
        kind, val = take()
        if kind == "op" and val == "(":
            inner = or_()
            if take() != ("op", ")"):
                raise Unconvertible(f"when {text.strip()[:60]!r}: missing )")
            return ("expr", f"({inner})")
        if kind == "str":
            return ("lit", json.dumps(val[1:-1]))
        if kind == "num":
            return ("lit", json.dumps(val))
        if kind == "word":
            if val in ("true", "false", "True", "False"):
                return ("expr", val.lower())
            if val == "inventory_hostname":
                return ("var", "(.Name)")
            if val in scope and val not in ("inventory_hostname", "ansible_managed"):
                return ("var", f"({scope[val]})")
            raise Unconvertible(f"when uses {val}, which is not a variable the converter knows (a fact or a result)")
        raise Unconvertible(f"when {text.strip()[:60]!r}: unexpected {val!r}")

    def cmp_():
        left = atom()
        if peek() == ("word", "is"):
            raise Unconvertible("'is defined' and other tests are not converted yet")
        if peek() == ("word", "in"):
            take()
            if take() != ("op", "["):
                raise Unconvertible("'in' needs a literal list")
            items = []
            while peek() != ("op", "]"):
                items.append(atom())
                if peek() == ("op", ","):
                    take()
            take()
            if not items:
                return ("expr", "false")
            def val(x):
                return x[1] if x[0] == "lit" else f'(printf "%v" {x[1]})'
            v = val(left)
            return ("expr", "or " + " ".join(f"(eq {v} {val(x)})" for x in items))
        if peek()[0] == "op" and peek()[1] in ("==", "!="):
            op = take()[1]
            right = atom()
            if left[0] == "lit" and right[0] != "lit":
                left, right = right, left
            def val(x):
                return x[1] if x[0] == "lit" else f'(printf "%v" {x[1]})'
            return ("expr", f"{'eq' if op == '==' else 'ne'} {val(left)} {val(right)}")
        if left[0] == "lit":
            return ("expr", "true")  # a bare string is truthy, as in Ansible
        return left if left[0] == "expr" else ("expr", left[1])

    out = or_()
    if i != len(toks):
        raise Unconvertible(f"when {text.strip()[:60]!r}: syntax the converter doesn't read")
    return out


def conv_text(text, scope):
    """Ansible's Jinja in one string, as Gorun's Go template. Raises Unconvertible when the text
    has Jinja that isn't a plain variable, or a variable the converter doesn't know."""
    if not JINJA_ANY.search(text):
        return text
    if JINJA_ANY.search(JINJA_VAR.sub("", text)):
        raise Unconvertible(f"Jinja that is not a plain variable: {text.strip()[:60]!r}")

    def sub(m):
        name = m.group(1)
        if name not in scope:
            raise Unconvertible(f"{name} is not known before this point (a fact, a registered result, or set later)")
        return "{{ " + scope[name] + " }}"

    return JINJA_VAR.sub(sub, text)


def main(argv=None):
    ap = argparse.ArgumentParser(description="Convert an Ansible inventory and playbooks into a Gorunfile.")
    ap.add_argument("playbooks", nargs="+", help="playbook files, such as site.yml")
    ap.add_argument("-i", "--inventory", action="append", required=True,
                    help="inventory file or directory (repeatable)")
    ap.add_argument("-o", "--out", default="gorun-out", help="output directory (default gorun-out)")
    ap.add_argument("--roles-path", action="append", default=[],
                    help="a roles directory (repeatable); defaults to roles/ and custom_roles/ next to each playbook")
    args = ap.parse_args(argv)

    roles = list(args.roles_path)
    for pb in args.playbooks:
        d = os.path.dirname(os.path.abspath(pb))
        roles += [os.path.join(d, "roles"), os.path.join(d, "custom_roles")]
        cfg = os.path.join(d, "ansible.cfg")
        if os.path.exists(cfg):
            with open(cfg, encoding="utf-8") as f:
                for line in f:
                    m = re.match(r"^\s*roles_path\s*=\s*(.+)$", line)
                    if m:
                        roles += [os.path.join(d, p.strip()) for p in m.group(1).split(":") if p.strip()]
    roles = [r for r in dict.fromkeys(roles) if os.path.isdir(r)]

    conv = Converter(args.inventory, args.playbooks, roles, args.out)
    conv.convert()
    path = conv.write(title=f"{' + '.join(os.path.basename(p) for p in args.playbooks)} (from Ansible)")
    c = conv.count
    print(f"wrote {path}", file=sys.stderr)
    print(f"  {c['plays']} plays, {c['roles']} roles, {c['tasks']} tasks converted, "
          f"{c['unconverted']} NOT CONVERTED (they fail on purpose), {len(conv.rep.items)} notes", file=sys.stderr)
    for where, text in conv.rep.items:
        print(f"  - {where}: {text}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
