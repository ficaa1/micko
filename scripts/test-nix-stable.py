#!/usr/bin/env python3
"""Check stable-channel creation, promotion and downgrade protection with local Git."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

UPDATER = Path(__file__).with_name("update-nix-stable.sh").resolve()


class StableChannel(unittest.TestCase):
    def test_release_promotion(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            repo = root / "repo"
            remote = root / "remote.git"
            tools = root / "bin"
            tools.mkdir()
            gh = tools / "gh"
            gh.write_text('#!/bin/sh\nprintf "%s\\n" "$TEST_LATEST"\n')
            gh.chmod(0o755)
            env = dict(os.environ, PATH=f"{tools}:{os.environ['PATH']}",
                       GITHUB_REPOSITORY="test/micko")

            def git(*args):
                return subprocess.check_output(["git", "-C", str(repo), *args],
                                               text=True, stderr=subprocess.DEVNULL).strip()

            subprocess.run(["git", "init", "--bare", str(remote)], check=True,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            subprocess.run(["git", "init", str(repo)], check=True,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            git("config", "user.name", "test")
            git("config", "user.email", "test@example.invalid")
            git("remote", "add", "origin", str(remote))
            (repo / "internal/buildinfo").mkdir(parents=True)
            (repo / "flake.nix").write_text("{}\n")
            (repo / "flake.lock").write_text("{}\n")

            def release(version):
                (repo / "internal/buildinfo/buildinfo.go").write_text(
                    f'const Version = "{version}"\n')
                git("add", ".")
                git("commit", "-m", version)
                git("tag", f"v{version}")
                git("push", "origin", f"refs/tags/v{version}")
                git("tag", "-d", f"v{version}")
                return git("rev-parse", "HEAD")

            def promote(tag, latest):
                subprocess.run(["bash", str(UPDATER)], cwd=repo,
                               env=dict(env, TAG=tag, TEST_LATEST=latest), check=True,
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

            def stable():
                return git("ls-remote", "origin", "refs/heads/stable").split()[0]

            first = release("0.8.0")
            promote("v0.8.0", "v0.8.0")
            self.assertEqual(stable(), first)
            promote("v0.8.0", "v0.8.0")
            self.assertEqual(stable(), first)
            release("0.9.0-rc.1")
            promote("v0.9.0-rc.1", "v0.9.0-rc.1")
            self.assertEqual(stable(), first)
            second = release("0.9.0")
            promote("v0.9.0", "v0.9.0")
            self.assertEqual(stable(), second)
            git("checkout", "v0.8.0")
            promote("v0.8.0", "v0.9.0")
            self.assertEqual(stable(), second)
            promote("v0.8.0", "v0.8.0")
            self.assertEqual(stable(), second)
            git("checkout", "v0.9.0")
            failed = subprocess.run(["bash", str(UPDATER)], cwd=repo,
                                    env=dict(env, TAG="v0.8.0", TEST_LATEST="v0.8.0"),
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            self.assertNotEqual(failed.returncode, 0)
            self.assertEqual(stable(), second)


if __name__ == "__main__":
    unittest.main()
