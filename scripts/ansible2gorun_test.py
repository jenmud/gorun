"""Tests for ansible2gorun. Run them with: python3 -m unittest scripts/ansible2gorun_test.py"""

import os
import shutil
import subprocess
import sys
import tempfile
import tomllib
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import ansible2gorun as a  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
DEMO = os.path.join(HERE, "testdata", "ansible-demo")
SCOPE = {"port": 'index .Server.Vars "port"', "env": 'index .Server.Vars "env"',
         "enabled": 'index .Server.Vars "enabled"'}


class ExpandPattern(unittest.TestCase):
    def test_numeric_range_keeps_zero_padding(self):
        self.assertEqual(a.expand_pattern("web[01:03]"), ["web01", "web02", "web03"])

    def test_plain_name_is_unchanged(self):
        self.assertEqual(a.expand_pattern("db-1.example.com"), ["db-1.example.com"])

    def test_letter_range(self):
        self.assertEqual(a.expand_pattern("n[a:c]"), ["na", "nb", "nc"])


class WhenTranslation(unittest.TestCase):
    def go(self, text):
        return a.when_to_go(text, SCOPE)

    def test_equality_compares_as_text(self):
        self.assertEqual(self.go('env == "prod"'),
                         'eq (printf "%v" (index .Server.Vars "env")) "prod"')

    def test_number_literal_is_compared_as_text(self):
        self.assertIn('"8080"', self.go("port == 8080"))

    def test_boolean_operators_and_not(self):
        out = self.go('not enabled and (env == "a" or env == "b")')
        self.assertTrue(out.startswith("and "), out)
        self.assertIn("not (", out)
        self.assertIn("or ", out)

    def test_in_a_literal_list(self):
        out = self.go("env in ['prod', 'staging']")
        self.assertEqual(out.count("eq "), 2)

    def test_facts_are_refused(self):
        with self.assertRaises(a.Unconvertible):
            self.go('ansible_os_family == "Debian"')

    def test_is_defined_is_refused(self):
        with self.assertRaises(a.Unconvertible):
            self.go("env is defined")


class TemplateText(unittest.TestCase):
    def test_plain_variable_becomes_a_gorun_expression(self):
        self.assertEqual(a.conv_text("listen {{ port }};", SCOPE),
                         'listen {{ index .Server.Vars "port" }};')

    def test_text_without_jinja_is_untouched(self):
        self.assertEqual(a.conv_text("echo {a: 1}", SCOPE), "echo {a: 1}")

    def test_filters_are_refused(self):
        with self.assertRaises(a.Unconvertible):
            a.conv_text("{{ port | default(80) }}", SCOPE)

    def test_unknown_names_are_refused(self):
        with self.assertRaises(a.Unconvertible):
            a.conv_text("{{ ansible_hostname }}", SCOPE)

    def test_block_jinja_is_refused(self):
        with self.assertRaises(a.Unconvertible):
            a.conv_text("{% if port %}x{% endif %}", SCOPE)


class Inheritance(unittest.TestCase):
    def test_a_value_can_use_another_in_the_same_layer(self):
        conv = a.Converter([], [], [], tempfile.mkdtemp())
        layer = {"deploy_root": "/srv", "release_dir": "{{ deploy_root }}/2.14"}
        self.assertEqual(conv.inline_refs(layer["release_dir"], layer), "/srv/2.14")


class InventoryAndPlaybooks(unittest.TestCase):
    """Converts the demo project in testdata/ansible-demo and checks the Gorunfile it writes."""

    @classmethod
    def setUpClass(cls):
        cls.out = tempfile.mkdtemp(prefix="ansible2gorun-test-")
        cwd = os.getcwd()
        try:
            os.chdir(DEMO)
            rc = a.main(["-i", "inventory/hosts.ini", "site.yml", "-o", cls.out])
        finally:
            os.chdir(cwd)
        assert rc == 0
        with open(os.path.join(cls.out, "Gorunfile"), encoding="utf-8") as f:
            cls.text = f.read()
        cls.doc = tomllib.loads(cls.text)  # the output must be valid TOML

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.out, ignore_errors=True)

    def hosts(self):
        return self.doc["servers"]["hosts"]

    def tasks(self):
        return self.doc["tasks"]

    def test_nothing_is_left_unconverted(self):
        bad = [t["description"] for t in self.tasks().values() if t["description"].startswith("NOT CONVERTED")]
        self.assertEqual(bad, [])

    def test_ranges_and_children_become_hosts_and_groups(self):
        self.assertEqual(sorted(self.hosts()), ["db-1", "web-1", "web-2", "web-3"])
        self.assertEqual(sorted(self.hosts()["web-2"]["groups"]), ["app", "web"])
        self.assertEqual(sorted(self.hosts()["db-1"]["groups"]), ["app", "db"])

    def test_host_vars_file_beats_the_inventory_line(self):
        self.assertEqual(self.hosts()["web-1"]["vars"]["http_port"], 8081)
        self.assertEqual(self.hosts()["web-2"]["vars"]["http_port"], "9090")

    def test_group_vars_reach_their_group(self):
        self.assertEqual(self.doc["server_groups"]["web"]["vars"]["nginx_worker_processes"], 2)

    def test_global_vars_from_group_vars_all(self):
        self.assertEqual(self.doc["vars"]["app_name"], "demo")

    def test_when_becomes_a_skipping_check(self):
        check = self.tasks()["web-tier-mark-production-hosts"]["check"]
        self.assertTrue(check.startswith('{{ if (eq (printf "%v" (index .Server.Vars "app_env")) "prod") }}'), check)

    def test_roles_handlers_and_includes_are_converted(self):
        self.assertIn("web-tier-handler-reload-web", self.tasks())
        self.assertIn("web-tier-mark-the-release-as-verified", self.tasks())
        self.assertEqual(self.hosts()["web-1"]["tasks"].count("web-tier-mark-the-release-as-verified"), 1)

    def test_a_loop_writes_one_task_per_item(self):
        names = [k for k in self.tasks() if k.startswith("web-tier-each-domain-gets-a-file")]
        self.assertEqual(len(names), 2)
        self.assertIn("example.test", self.tasks()[names[0]]["cmd"] + self.tasks()[names[1]]["cmd"])

    def test_copied_files_are_shipped_next_to_the_gorunfile(self):
        shipped = os.listdir(os.path.join(self.out, "files"))
        self.assertTrue(any(name.endswith("index.html") for name in shipped), shipped)
        self.assertTrue(any(name.endswith("motd") for name in shipped), shipped)

    def test_secrets_are_never_written(self):
        self.assertNotIn("password", self.text.lower())


@unittest.skipUnless(os.environ.get("GORUN"), "set GORUN to a gorun binary to load the output with it")
class LoadsInGorun(unittest.TestCase):
    def test_the_demo_output_loads(self):
        out = tempfile.mkdtemp(prefix="ansible2gorun-load-")
        try:
            cwd = os.getcwd()
            os.chdir(DEMO)
            try:
                a.main(["-i", "inventory/hosts.ini", "site.yml", "-o", out])
            finally:
                os.chdir(cwd)
            res = subprocess.run([os.environ["GORUN"], os.path.join(out, "Gorunfile"), "--list-tasks", "-plain"],
                                 capture_output=True, text=True, env={**os.environ, "GORUN_SKIP_NATS": "1"})
            self.assertEqual(res.returncode, 0, res.stderr)
        finally:
            shutil.rmtree(out, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
