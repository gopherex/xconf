#!/usr/bin/env python3
"""Exercise the real release recipe against disposable modules and a local remote."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
MODULE = "github.com/gopherex/xconf"
ADAPTER = "contrib/sources/fixture"


class ReleaseTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="xconf-release-test-")
        self.addCleanup(self.temp.cleanup)
        base = Path(self.temp.name)
        self.repo = base / "checkout"
        self.remote = base / "remote.git"
        self.repo.mkdir()
        self.env = dict(os.environ, GOWORK="off", GIT_CONFIG_GLOBAL=os.devnull,
                        GIT_CONFIG_NOSYSTEM="1", GIT_TERMINAL_PROMPT="0",
                        GIT_ALLOW_PROTOCOL="file")
        shutil.copyfile(ROOT / "Makefile", self.repo / "Makefile")
        self.write("go.mod", f"module {MODULE}\n\ngo 1.25.7\n")
        self.write("config.go", "package xconf\nconst Value = 7\n")
        self.write(f"{ADAPTER}/go.mod", f"""module {MODULE}/{ADAPTER}

go 1.25.7

require {MODULE} v1.2.0
replace {MODULE} => ../../..
""")
        self.write(f"{ADAPTER}/source.go", f"""package fixture
import x "{MODULE}"
const Value = x.Value
""")
        self.write("example/go.mod", f"""module {MODULE}/example

go 1.25.7

require (
    {MODULE} v1.2.0
    {MODULE}/{ADAPTER} v1.2.0
)
replace {MODULE} => ..
replace {MODULE}/{ADAPTER} => ../{ADAPTER}
""")
        self.write("example/main.go", f"""package main
import (
    x "{MODULE}"
    source "{MODULE}/{ADAPTER}"
)
func main() {{ if x.Value != source.Value {{ panic("mismatch") }} }}
""")
        self.run_command("make", "tidy")
        self.git("init", "-b", "master")
        self.git("config", "user.name", "Release test")
        self.git("config", "user.email", "release-test@example.invalid")
        self.git("config", "core.hooksPath", os.devnull)
        self.git("config", "commit.gpgsign", "false")
        self.git("config", "tag.gpgsign", "false")
        self.git("add", "-A")
        self.git("commit", "-m", "test: seed release fixture")
        self.git("tag", "v1.2.0")
        self.git("tag", f"{ADAPTER}/v1.2.0")
        self.git("init", "--bare", str(self.remote))
        self.git("remote", "add", "origin", str(self.remote))
        self.git("push", "origin", "master", "--tags")

    def write(self, name, text):
        path = self.repo / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def run_command(self, *args, input=None, check=True):
        result = subprocess.run(args, cwd=self.repo, env=self.env, input=input,
                                text=True, stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, timeout=180)
        if check and result.returncode:
            self.fail(f"{args}:\n{result.stdout}")
        return result

    def git(self, *args):
        return self.run_command("git", *args).stdout.strip()

    def break_tests(self):
        self.write("config_test.go", """package xconf
import "testing"
func TestReleaseGate(t *testing.T) { t.Fatal("release must stop") }
""")
        self.git("add", "config_test.go")
        self.git("commit", "-m", "test: fail release checks")

    def assert_release_stops(self, answers):
        head = self.git("rev-parse", "HEAD")
        refs = self.git("show-ref", "--tags")
        remote = self.git("ls-remote", "origin")
        result = self.run_command("make", "release", input=answers, check=False)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn("release must stop", result.stdout)
        self.assertEqual(self.git("rev-parse", "HEAD"), head)
        self.assertEqual(self.git("show-ref", "--tags"), refs)
        self.assertEqual(self.git("ls-remote", "origin"), remote)

    def test_bump_updates_examples_without_tagging_them(self):
        self.run_command("make", "release", input="2\n3\nyes\n")
        for directory in (ADAPTER, "example"):
            result = self.run_command("go", "mod", "edit",
                                      f"-modfile={directory}/go.mod", "-json")
            required = json.loads(result.stdout)["Require"]
            self.assertTrue(required)
            self.assertTrue(all(dep["Version"] == "v1.2.1" for dep in required))
        self.run_command("make", "test")
        self.run_command("make", "tidy")
        self.assertEqual(self.git("status", "--porcelain"), "")
        self.assertEqual(self.git("tag", "-l", "*/v1.2.1"), f"{ADAPTER}/v1.2.1")
        head = self.git("rev-parse", "HEAD")
        self.assertEqual(self.git("rev-parse", "v1.2.1^{}"), head)
        self.assertEqual(self.git("rev-parse", f"{ADAPTER}/v1.2.1^{{}}"), head)
        self.assertIn(head, self.git("ls-remote", "origin", "refs/heads/master"))
        self.assertIn(head, self.git("ls-remote", "origin", "refs/tags/v1.2.1^{}"))
        self.assertEqual(self.git("ls-remote", "origin", "refs/tags/example/*"), "")

    def test_failed_checks_prevent_bump_publication(self):
        self.break_tests()
        self.assert_release_stops("2\n3\nyes\n")

    def test_failed_checks_prevent_tag_recreation(self):
        self.break_tests()
        self.assert_release_stops("1\nyes\n")


if __name__ == "__main__":
    unittest.main()
