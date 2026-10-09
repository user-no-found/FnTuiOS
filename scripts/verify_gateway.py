"""Run both real-browser public-authority policy modes against isolated workers."""
import concurrent.futures
import os
from pathlib import Path
import socket
import ssl
import time
import urllib.request

from buildflow.runner import Runner
from build_inputs import ROOT, capped, run, temporary_environment


def verify(args, app, output, harness, profiles=None):
    """Use the same packaged binaries with strict and repaired runtime settings."""
    for profile, policy in [(profile, policy) for profile in (profiles or ['desktop', 'mobile'])
                            for policy in ['strict', 'repaired']]:
        directory = Path(output) / profile / policy
        directory.mkdir(parents=True, exist_ok=True)
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            host = f'127.0.0.1:{sock.getsockname()[1]}'
        env = {**os.environ, **temporary_environment(), 'FNTUIOS_E2E_NATIVE': '1',
               'FNTUIOS_E2E_BIN_DIR': str(app / 'bin'), 'FNTUIOS_E2E_BROWSER_HOST': host,
               'FNTUIOS_E2E_REMOTE_POLICY': policy}
        runner = Runner(directory / 'fixture.log')
        url = 'https://' + host
        tls = ssl._create_unverified_context()  # disposable httptest certificate, loopback only
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
            future = pool.submit(runner.run, capped([harness, '-test.run', '^TestNativeBrowser$',
                                 '-test.v', '-test.timeout', '3m']), cwd=ROOT, env=env)
            try:
                deadline = time.monotonic() + 40
                while time.monotonic() < deadline:
                    if future.done():
                        future.result()
                        raise ValueError('remote gateway fixture exited before readiness')
                    try:
                        urllib.request.urlopen(url, timeout=1, context=tls).close()
                        break
                    except OSError:
                        time.sleep(.2)
                else:
                    raise ValueError('remote gateway fixture readiness timed out')
                command = ['node', ROOT / 'scripts/verify_gateway_browser.mjs', '--url', url,
                     '--output', directory, '--policy', policy, '--playwright', args.playwright,
                     '--chromium', args.chromium]
                if profile == 'mobile':
                    command.append('--mobile')
                run(command)
            finally:
                try:
                    urllib.request.urlopen(urllib.request.Request(url + '/__fixture_stop', method='POST'),
                                           timeout=2, context=tls).close()
                except OSError:
                    pass
                future.result(timeout=45)
