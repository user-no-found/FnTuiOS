"""Supply owned executable temporary storage outside every Git checkout."""
import atexit
import json
import os
from pathlib import Path
import shutil
import tempfile


class TemporaryRunner:
    """Forward commands to the shared runner with one private temporary root."""

    def __init__(self, runner, record_directory):
        self.runner = runner
        record_directory = Path(record_directory)
        if record_directory.is_symlink():
            raise ValueError('Build temporary record directory must not be a symlink')
        record_directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        if record_directory.stat().st_uid != os.geteuid():
            raise ValueError('Build temporary records must belong to the current user')
        record_directory.chmod(0o700)
        self.directory = Path(tempfile.mkdtemp(prefix='fntuios-', dir='/tmp')).resolve()
        self.directory.chmod(0o700)
        self.record = record_directory / 'owner.json'
        self.environment = {'TMPDIR': str(self.directory), 'GOTMPDIR': str(self.directory)}
        self.closed = False
        atexit.register(self.close)
        try:
            self._write_record('active')
            for parent in [self.directory, *self.directory.parents]:
                if (parent / '.git').exists() or (parent / '.git').is_symlink():
                    raise ValueError(f'Build temporary storage has a Git ancestor: {parent}')
            probe = self.directory / 'exec-probe'
            probe.write_text('#!/bin/sh\nexit 0\n')
            probe.chmod(0o700)
            self.run([probe], cwd=self.directory)
            probe.unlink()
        except BaseException as error:
            self.close()
            if isinstance(error, (KeyboardInterrupt, SystemExit)):
                raise
            raise ValueError('Private /tmp build storage must allow native execution outside a Git checkout') from error

    def _write_record(self, status):
        self.record.write_text(json.dumps({'directory': str(self.directory), 'uid': os.geteuid(),
            'pid': os.getpid(), 'status': status, 'environment': self.environment}, indent=2) + '\n')

    def close(self):
        """Remove only this invocation's storage; logs remain in the work directory."""
        if not self.closed:
            shutil.rmtree(self.directory)
            self.closed = True
            self._write_record('cleaned')

    def run(self, argv, cwd, env=None):
        return self.runner.run(argv, cwd=cwd, env={**(env or {}), **self.environment})

    def capture(self, argv, cwd, env=None):
        return self.runner.capture(argv, cwd=cwd, env={**(env or {}), **self.environment})
