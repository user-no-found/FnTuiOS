"""Run the native single-UID browser fixture and its real clipboard regressions."""
import concurrent.futures
import json
import os
import socket
import time
import threading
import urllib.request

from buildflow.contracts import BuildError
from buildflow.browser import resolve_browser
from buildflow.runner import Runner
from build_inputs import ROOT, capped, run, write_json, temporary_environment


def browser_check(args, app, output, harness, negative=False, panel=False):
    tools = resolve_browser({"playwright": args.playwright, "chromium": args.chromium})
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
            if panel:
                command = [tools.node, ROOT / 'scripts/verify_runtime_panel.mjs', '--url', url,
                           '--output', output / 'browser']
            else:
                command = [tools.node, ROOT / 'scripts/verify_browser.mjs', '--url', url,
                           '--output', output / 'browser', '--playwright', args.playwright,
                           '--chromium', args.chromium, '--quiet']
                if negative:
                    command.append('--negative-image-key')
            run(command, env=tools.environment())
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
    failure_path = directory / 'browser/failure.json'
    failure_path.unlink(missing_ok=True)
    expected = 'keyboard.fallback.swallowed'
    try:
        browser_check(args, app, directory, harness, negative=True)
    except BuildError as error:
        if error.exit_code == 130:
            raise
        if not failure_path.is_file():
            raise ValueError(f'keyboard negative control produced no failure evidence: {error}') from error
        failure = json.loads(failure_path.read_text())
        evidence = failure.get('evidence') or {}
        mutation = failure.get('mutation') or {}
        keys = evidence.get('events') or []
        frames = [key.get('encoded') if key.get('encoded') is not None else
                  ('\x16' if key.get('type') == 'keydown' else None) for key in keys]
        frames = [frame for frame in frames if frame]
        checks = {
            'failure_code': failure.get('code') == expected,
            'browser_errors': not failure.get('failures'),
            'injected_mutation': mutation == {'id': 'disable-native-paste-key', 'applied': True},
            'event_sequence': [(key.get('type'), key.get('repeat')) for key in keys] ==
                              [('keydown', False), ('keydown', True), ('keyup', False)],
            'expected_frames': bool(frames) and evidence.get('expected') == frames,
            'missing_frames': evidence.get('actual') == [],
        }
        rejected = [name for name, matched in checks.items() if not matched]
        if rejected:
            raise ValueError(f"keyboard negative control expected {expected}; observed "
                             f"{failure.get('code')}: {failure.get('error')}; "
                             f"failed checks: {', '.join(rejected)}; evidence: {failure_path}") from error
    else:
        raise ValueError('keyboard negative control unexpectedly passed')
    write_json(directory / 'control.json', {'mutation': 'disable native paste key hook in served asset',
               'expected_code': expected, 'observed_error': failure['error'],
               'expected_frames': evidence['expected'], 'actual_frames': evidence['actual'], 'verified': True})
