#!/usr/bin/env python3
"""Exercise a packaged prepare-state command in an explicit root-only /tmp fixture."""

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile


def metadata(path):
    info = path.lstat()
    return info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)


def verify(binary, template, uid):
    if os.geteuid() != 0:
        raise ValueError('this isolated ownership fixture requires explicit root execution')
    if uid < 1000 or uid in (65534, 4294967295):
        raise ValueError('--uid must select an existing non-system NAS account')
    environment = {'PATH': '/usr/bin:/bin', 'LANG': 'C'}
    account = subprocess.run(['/usr/bin/getent', 'passwd', str(uid)], env=environment,
                             text=True, capture_output=True, check=True).stdout.strip().split(':')
    if len(account) != 7 or int(account[2]) != uid:
        raise ValueError('NSS did not return the requested fixture identity')
    gid = int(account[3])
    workspace = Path(tempfile.mkdtemp(prefix='fntuios-state-proof-', dir='/tmp'))
    workspace.chmod(0o711)
    root = workspace / 'fixture'
    root.mkdir(mode=0o711)
    root.chmod(0o711)
    checks = []
    report = {'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(), 'uid': uid,
              'checks': checks, 'verified': False,
              'boundary': 'real UID ownership in isolated /tmp paths; no installed paths, callbacks or services'}
    try:
        app, state, runtime = [root / name for name in ('app', 'state', 'run')]
        for directory in (app, state, runtime):
            directory.mkdir(mode=0o711)
            directory.chmod(0o711)
        config = json.loads(template.read_text())
        config.update(app_dir=str(app), state_dir=str(state), run_dir=str(runtime),
                      gateway_account='nobody', trusted_gateway_accounts=['root'])
        config.pop('state_restore_max_entries', None)
        config.pop('state_restore_max_depth', None)
        policy = root / 'gateway.json'
        policy.write_text(json.dumps(config))
        policy.chmod(0o600)
        control = state / 'service.log'
        control.write_bytes(b'control must remain root owned\n')
        control.chmod(0o600)
        private = state / str(uid)
        private.mkdir(mode=0o700)
        child = private / 'state'
        child.mkdir(mode=0o700)
        sample = child / 'persisted.bin'
        sample.write_bytes(b'isolated persisted state\x00\x01\x02\n')
        sample.chmod(0o700)
        before = hashlib.sha256(sample.read_bytes()).hexdigest()

        def command(expected_success, expected_error=None):
            result = subprocess.run([str(binary), 'prepare-state', '--config', str(policy)],
                                    cwd=root, env=environment, text=True, capture_output=True, timeout=30)
            if (result.returncode == 0) != expected_success:
                raise AssertionError('prepare-state result disagrees with fixture expectation')
            if expected_error and expected_error not in result.stderr:
                raise AssertionError('prepare-state did not report the expected refusal category')

        lock = runtime / 'supervisor.lock'
        with lock.open('w') as held:
            lock.chmod(0o600)
            fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
            command(False, 'offline supervisor')
            if metadata(private)[:2] != (0, 0) or metadata(sample)[:2] != (0, 0):
                raise AssertionError('active supervisor refusal changed fixture ownership')
        checks.append('active supervisor excludes restoration without changing ownership')

        lock.unlink()
        os.mkfifo(lock, 0o600)
        command(False, 'unsafe supervisor lock file')
        lock.unlink()
        checks.append('a special supervisor lock file is refused without blocking')

        outside = root / 'outside.bin'
        outside.write_bytes(b'outside ownership sentinel\n')
        outside.chmod(0o600)
        link = child / 'hardlink'
        os.link(outside, link)
        outside_before = metadata(outside)
        command(False)
        if metadata(outside) != outside_before:
            raise AssertionError('hardlink rejection changed an outside inode')
        link.unlink()
        checks.append('hardlinks are refused without transferring the outside inode')

        symbolic = child / 'symbolic'
        symbolic.symlink_to(outside)
        command(True)
        for path in (private, child, sample):
            if metadata(path) != (uid, gid, 0o700):
                raise AssertionError('nested fixture ownership or mode differs from the expected result')
        if hashlib.sha256(sample.read_bytes()).hexdigest() != before:
            raise AssertionError('state restoration changed persisted fixture bytes')
        if metadata(outside) != outside_before or metadata(symbolic)[0] != 0 or metadata(control)[0] != 0:
            raise AssertionError('restoration followed a symlink or reassigned a control file')
        checks.append('absent deployment limits use defaults and nested ownership is restored with modes and bytes preserved')
        checks.append('symlink target, symlink owner and root control file remain unchanged')

        command(True)
        if metadata(sample) != (uid, gid, 0o700):
            raise AssertionError('repeated preparation changed a user-owned tree')
        checks.append('preparation is idempotent for an already user-owned tree')

        def become_user():
            os.setgroups([])
            os.setgid(gid)
            os.setuid(uid)

        result = subprocess.run(['/bin/sh', '-c', 'test -r "$1" && test -w "$1"', 'fixture', str(sample)],
                                cwd='/tmp', env=environment, preexec_fn=become_user, timeout=10)
        if result.returncode:
            raise AssertionError('the real selected UID cannot access its restored fixture state')
        checks.append('the actual selected UID can read and write its restored state')
        report['verified'] = True
    finally:
        shutil.rmtree(root)
        proof = workspace / 'verified.json'
        proof.write_text(json.dumps(report, indent=2) + '\n')
        proof.chmod(0o644)
        print(proof)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=lambda value: Path(value).resolve(), required=True)
    parser.add_argument('--template', type=lambda value: Path(value).resolve(), required=True)
    parser.add_argument('--uid', type=int, required=True)
    args = parser.parse_args()
    verify(args.binary, args.template, args.uid)


if __name__ == '__main__':
    main()
