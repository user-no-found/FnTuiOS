"""Resolve latest upstream code and a verified Go toolchain for one build."""
import hashlib
from contextlib import nullcontext
import json
import os
from pathlib import Path
import re
import tempfile
from build_temporary import TemporaryRunner
import urllib.request
from safe_archive import extract_archive

ROOT = Path(__file__).resolve().parents[1]


_runner = None
_context = None


def bind_context(context):
    """Send semantic build actions through the shared execution context."""
    global _context
    _context = context


def activity(label, *, target=''):
    """Report nested work when called by a pipeline; standalone tools stay quiet."""
    return _context.activity(label, target=target) if _context is not None else nullcontext()


def status(message):
    """Update the shared current action without printing captured tool output."""
    if _context is not None:
        _context.status(message)


def bind_runner(runner):
    """Use the pipeline's execution/logging boundary for all application commands."""
    global _runner
    _runner = runner


def command_runner():
    global _runner
    if _runner is None:
        from buildflow.runner import Runner
        work = ROOT / '.build'
        work.mkdir(exist_ok=True)
        temporary = Path(tempfile.mkdtemp(prefix='tools-', dir=work))
        _runner = TemporaryRunner(Runner(temporary / 'tools.log'), temporary / 'tmp')
    return _runner


def run(args, cwd=None, capture=False, env=None):
    runner = command_runner()
    method = runner.capture if capture else runner.run
    result = method([str(a) for a in args], cwd=Path(cwd or ROOT), env=env)
    return result if capture else None


def capped(args):
    cpus = sorted(os.sched_getaffinity(0))
    return ['nice', '-n', '10', 'taskset', '-c', ','.join(map(str, cpus[:min(8, max(1, len(cpus)//2))])), *args]


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def write_json(path, value):
    Path(path).parent.mkdir(parents=True, exist_ok=True)
    Path(path).write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def required_go_version(source):
    """Honor both the minimum language version and the preferred Go toolchain."""
    text = (Path(source) / 'go.mod').read_text()
    language = re.search(r'^go (\d+\.\d+(?:\.\d+)?)$', text, re.M)
    if not language:
        raise ValueError('upstream does not specify a supported Go version')
    candidates = [tuple(map(int, (language[1].split('.') + ['0'])[:3]))]
    directive = re.search(r'^toolchain\s+(.+)$', text, re.M)
    if directive and directive[1] != 'default':
        tool = re.fullmatch(r'go(\d+)\.(\d+)\.(\d+)', directive[1])
        if not tool:
            raise ValueError('unsupported upstream toolchain directive: ' + directive[1])
        candidates.append(tuple(map(int, tool.groups())))
    return 'go' + '.'.join(map(str, max(candidates)))


def toolchain(source):
    version = required_go_version(source)
    target = ROOT / '.cache/toolchains' / version
    executable = target / 'go/bin/go'
    listing = json.load(urllib.request.urlopen('https://go.dev/dl/?mode=json&include=all'))
    release = next(v for v in listing if v['version'] == version)
    asset = next(f for f in release['files'] if f['os'] == 'linux' and f['arch'] == 'amd64' and f['kind'] == 'archive')
    archive = target.parent / asset['filename']
    archive_url = 'https://dl.google.com/go/' + asset['filename']
    if not archive.exists() or digest(archive) != asset['sha256']:
        archive.parent.mkdir(parents=True, exist_ok=True)
        urllib.request.urlretrieve(archive_url, archive)
    if digest(archive) != asset['sha256']:
        raise ValueError('Go toolchain archive checksum mismatch')
    if not executable.exists():
        target.mkdir(parents=True, exist_ok=True)
        extract_archive(archive, target)
    actual = run([executable, 'version'], capture=True).strip()
    if not actual.startswith('go version ' + version + ' linux/amd64'):
        raise ValueError('cached Go executable does not match required toolchain')
    return executable, {'version': version, 'archive_sha256': asset['sha256'], 'archive_url': archive_url}


def temporary_environment():
    return getattr(command_runner(), 'environment', {})


def temporary_directory():
    return temporary_environment().get('TMPDIR')


def goenv():
    env = {**os.environ, **temporary_environment()}
    env.update({'GOTOOLCHAIN': 'local', 'CGO_ENABLED': '0', 'GOMAXPROCS': str(max(1, len(os.sched_getaffinity(0))//2))})
    return env
