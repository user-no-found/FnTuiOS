"""Run the native single-UID browser fixture and its real clipboard regressions."""
import concurrent.futures
import json
import os
import socket
import time
import threading
import urllib.request

from buildflow.contracts import BuildError
from buildflow.runner import Runner
from build_inputs import ROOT, capped, run, write_json, temporary_environment


def browser_check(args, app, output, harness, negative=False):
    output.mkdir(parents=True, exist_ok=True)
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        host = f'127.0.0.1:{sock.getsockname()[1]}'
    env = {**os.environ, **temporary_environment(), 'FNTUIOS_E2E_NATIVE': '1', 'FNTUIOS_E2E_BIN_DIR': str(app / 'bin'),
           'FNTUIOS_E2E_BROWSER_HOST': host}
    cancelled = threading.Event()
    def observe(event):
        if cancelled.is_set():
            raise BuildError('native browser fixture cancelled', 130)
    runner = Runner(output / 'browser-fixture.log', observe)
    command = capped([harness, '-test.run', '^TestNativeBrowser$', '-test.v', '-test.timeout', '6m'])
    url = 'http://' + host
    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as executor:
        future = executor.submit(runner.run, command, cwd=ROOT, env=env)
        try:
            deadline = time.monotonic() + 45
            while time.monotonic() < deadline:
                if future.done():
                    future.result()
                    raise ValueError('native browser fixture exited before readiness')
                try:
                    urllib.request.urlopen(url, timeout=1).close()
                    break
                except OSError:
                    time.sleep(0.2)
            else:
                raise ValueError('native browser fixture readiness timed out')
            command = ['node', ROOT / 'scripts/verify_browser.mjs', '--url', url,
                       '--output', output / 'browser', '--playwright', args.playwright,
                       '--chromium', args.chromium, '--quiet']
            if negative:
                command.append('--negative-image-key')
            run(command)
        finally:
            try:
                urllib.request.urlopen(urllib.request.Request(url + '/__fixture_stop', method='POST'), timeout=2).close()
            except OSError:
                pass
            try:
                future.result(timeout=45)
            except concurrent.futures.TimeoutError:
                cancelled.set()
                future.result(timeout=15)


def negative_check(args, app, output, harness):
    directory = output / 'negative-image-key'
    try:
        browser_check(args, app, directory, harness, negative=True)
    except BuildError:
        failure = json.loads((directory / 'browser/failure.json').read_text())
        if failure.get('error') != 'native text paste was lost':
            raise ValueError('keyboard negative control failed for an unrelated reason')
    else:
        raise ValueError('keyboard negative control unexpectedly passed')
    write_json(directory / 'control.json', {'mutation': 'disable native paste key hook in served asset',
               'expected_failure': 'native text paste was lost', 'verified': True})
