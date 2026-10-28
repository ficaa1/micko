#!/usr/bin/env python3
"""Exercise the disposable runner's ownership and cleanup boundaries with fake tools."""

import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

MANIFEST = Path(sys.argv.pop(1)).resolve()
if hashlib.sha256(MANIFEST.read_bytes()).hexdigest() != "adac8370b9c0470acd3a71cf4139d40d7b8e995a774c1a639864b3db0e601a83":
    raise SystemExit("Expected the official Argo v4.1.2 install.yaml fixture")
RUNNER = Path(__file__).with_name("argo.sh").resolve()

FAKE_TOOL = '''#!/usr/bin/env python3
import json, os, pathlib, shutil, sys
name=pathlib.Path(sys.argv[0]).name
args=sys.argv[1:]
with open(os.environ['COMPAT_CALLS'],'a') as out:
    out.write(json.dumps([name,args])+'\\n')
if name=='kind':
    if args[:2]==['get','clusters']: sys.exit(0)
    if args[:2]==['create','cluster'] and os.environ['COMPAT_FAIL']=='create': sys.exit(23)
if name=='curl':
    target=pathlib.Path(args[args.index('-o')+1])
    if os.environ['COMPAT_FAIL']=='checksum': target.write_text('corrupt')
    else: shutil.copyfile(os.environ['COMPAT_MANIFEST'],target)
if name=='kubectl' and 'token' in args: print('fixture-sensitive-token')
if name=='openssl':
    for flag in ['-keyout','-out']:
        if flag in args: pathlib.Path(args[args.index(flag)+1]).write_text('fixture')
if name=='go':
    config=pathlib.Path(os.environ['MICKO_COMPAT_CONFIG']).read_text()
    assert 'currentProfile: direct' in config and 'kind-micko-compat-' in config
    assert 'insecureSkipTLSVerify' not in config
    assert os.environ['MICKO_COMPAT']=='1'
    assert pathlib.Path(os.environ['MICKO_COMPAT_TOKEN']).read_text().strip()=='fixture-sensitive-token'
    if os.environ['COMPAT_FAIL']=='test': sys.exit(24)
'''


class DisposableRunner(unittest.TestCase):
    def test_ownership_and_cleanup(self):
        for failure in ["", "create", "checksum", "test"]:
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                tools = root / "tools"
                tools.mkdir()
                for name in ["docker", "kind", "kubectl", "go", "curl", "openssl"]:
                    path = tools / name
                    path.write_text(FAKE_TOOL)
                    path.chmod(0o755)
                calls = root / "calls.jsonl"
                env = os.environ | {
                    "PATH": str(tools) + os.pathsep + os.environ["PATH"],
                    "TMPDIR": str(root),
                    "COMPAT_CALLS": str(calls),
                    "COMPAT_FAIL": failure,
                    "COMPAT_MANIFEST": str(MANIFEST),
                }
                original_config = root / "user-kubeconfig"
                original_config.write_text("user state must survive")
                env["KUBECONFIG"] = str(original_config)
                result = subprocess.run([str(RUNNER)], env=env, text=True, capture_output=True, timeout=30)
                self.assertEqual(result.returncode == 0, failure == "", result.stderr)
                self.assertNotIn("fixture-sensitive-token", result.stdout + result.stderr)
                self.assertEqual(original_config.read_text(), "user state must survive")
                self.assertFalse(list(root.glob("micko-compat.*")), "temporary credentials survived")
                log = [json.loads(line) for line in calls.read_text().splitlines()]
                creates = [args for tool, args in log if tool == "kind" and args[:2] == ["create", "cluster"]]
                deletes = [args for tool, args in log if tool == "kind" and args[:2] == ["delete", "cluster"]]
                self.assertEqual(len(creates), 1)
                self.assertEqual(len(deletes), 1)
                self.assertEqual(creates[0][creates[0].index("--name") + 1], deletes[0][deletes[0].index("--name") + 1])
                self.assertIn("@sha256:", creates[0][creates[0].index("--image") + 1])
                for tool, args in log:
                    if tool == "kubectl":
                        self.assertIn("--kubeconfig", args)
                        self.assertIn("--context", args)
                        self.assertTrue(args[args.index("--context") + 1].startswith("kind-micko-compat-"))
                runs = [args for tool, args in log if tool == "go"]
                self.assertEqual(len(runs), 1 if failure in ["", "test"] else 0)
                if runs:
                    self.assertIn("-tags=compat", runs[0])


if __name__ == "__main__":
    unittest.main()
