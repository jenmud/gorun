# Demo project for ansible2gorun

A small Ansible project written for the converter's tests. Every host uses
`ansible_connection=local`, so it runs on one machine, and it writes only under
`/tmp/ansible2gorun-demo`.

It uses what real playbooks use: groups and children, host ranges, group_vars, host_vars,
vars_files, roles with defaults, vars and dependencies, handlers, templates, a copied file,
include_tasks, loops over variables, when, creates, and an assert.

```bash
cd scripts/testdata/ansible-demo
python3 ../../ansible2gorun.py -i inventory/hosts.ini site.yml -o /tmp/demo-gorun
cd /tmp/demo-gorun && gorun Gorunfile -r -plain
```

Expect 0 NOT CONVERTED tasks, and these files under `/tmp/ansible2gorun-demo`: `motd`,
`www/web-1/vhost.conf` (listening on 8081, from host_vars), `releases/2.14/web-1/VERIFIED`, and no
`env` file for `web-3`, which is a staging host.
