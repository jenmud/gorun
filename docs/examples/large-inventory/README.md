# Large inventory (mock)

A mock of a split Gorunfile for a large fleet, shaped like an Ansible inventory:
`tasks/` are roles, `groups/` are `group_vars`, `hosts/` is the inventory.

> **Proposal, not runnable.** The top-level `include` key below is not implemented yet.
> Until it is, merge the files by hand or use them as a layout reference. Every other key
> is current gorun syntax (see [gorunfile.md](../../gorunfile.md)).

```
large-inventory/
├── Gorunfile          # include list, globals, [servers] defaults
├── tasks/             # ≈ roles: [tasks.*] and [bundles.*]
├── groups/            # ≈ group_vars: [server_groups.*]
└── hosts/             # ≈ inventory: one line per host
```

Rules the mock assumes:

- Paths in `include` are relative to the file that names them.
- A name defined in two files is an error, so file order never changes a value.
- Layering is unchanged: global, then group, then bundle, then host.

The hosts file lists six machines as a sample. A real 69-host fleet would have the same shape.
