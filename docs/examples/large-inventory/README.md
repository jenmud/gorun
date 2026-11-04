# Large inventory (mock)

A split Gorunfile for a large fleet, shaped like an Ansible inventory: `tasks/` are roles,
`groups/` are `group_vars`, `hosts/` is the inventory. The root `Gorunfile` pulls the other
files in with [`include`](../../gorunfile.md#splitting-a-large-gorunfile).

Check it without connecting to anything:

```bash
gorun docs/examples/large-inventory/Gorunfile --list-servers
gorun docs/examples/large-inventory/Gorunfile --list-tasks
```

```
large-inventory/
├── Gorunfile          # include list, globals, [servers] defaults
├── tasks/             # ≈ roles: [tasks.*] and [bundles.*]
├── groups/            # ≈ group_vars: [server_groups.*]
└── hosts/             # ≈ inventory: one line per host
```

Rules that apply (see the [full list](../../gorunfile.md#splitting-a-large-gorunfile)):

- Paths in `include` are relative to the file that names them.
- A name defined in two files is an error, so file order never changes a value.
- Layering is unchanged: global, then group, then bundle, then host.

The hosts file lists six machines as a sample. A real 69-host fleet would have the same shape.
