"""Application input identity and explicit local Go runtime dependency resolution."""
import hashlib
import os
from pathlib import Path
import shutil

from build_inputs import ROOT, digest, run


def inputs_digest():
    paths = [ROOT / 'project.json', ROOT / 'LICENSE', ROOT / 'build.sh', ROOT / 'setup.sh']
    for directory in ['scripts', 'tools', 'locales', 'localization', 'packaging', 'cmd', 'internal', 'assets', 'adapters']:
        paths.extend(p for p in (ROOT / directory).rglob('*') if p.is_file() and '__pycache__' not in str(p))
    paths.extend(p for p in [ROOT / 'go.mod', ROOT / 'go.sum'] if p.exists())
    return hashlib.sha256(''.join(str(p.relative_to(ROOT)) + ':' + digest(p) for p in sorted(paths)).encode()).hexdigest()


def tree_digest(root):
    paths = [p for p in root.rglob('*') if p.is_file() and not any(part in {'.git', 'tasks', '__pycache__', '.build', 'output', '.cache'} or part.endswith('.egg-info') for part in p.relative_to(root).parts)]
    return hashlib.sha256(''.join(str(p.relative_to(root)) + ':' + digest(p) for p in sorted(paths)).encode()).hexdigest()


def runtime_modfile(go, work, env):
    """Resolve the shared runtime without persisting a machine-specific Go replace."""
    configured = os.environ.get('FNPACKKIT_PATH')
    if not configured:
        raise ValueError('FNPACKKIT_PATH is required for the unpublished Go runtime dependency')
    toolkit = Path(configured).resolve()
    runtime = toolkit / 'runtime'
    if not (runtime / 'go.mod').is_file():
        raise ValueError('FNPACKKIT_PATH/runtime/go.mod is missing')
    modfile = work / 'fntuios.mod'
    shutil.copy2(ROOT / 'go.mod', modfile)
    run([go, 'mod', 'edit', '-modfile=' + str(modfile), '-replace=github.com/user-no-found/FnPackKit/runtime=' + str(runtime)], cwd=ROOT, env=env)
    run([go, 'mod', 'tidy', '-modfile=' + str(modfile)], cwd=ROOT, env=env)
    identity = {'module': 'github.com/user-no-found/FnPackKit/runtime', 'mode': 'explicit-local-checkout',
                'path': str(runtime), 'source_tree_sha256': tree_digest(runtime), 'modfile_sha256': digest(modfile)}
    return modfile, identity


def tools_identity():
    import buildflow
    import fnpackkit
    from buildflow.metadata import version
    from fnpackkit.metadata import tool_version
    result = {}
    for name, module, release in [('buildflow', buildflow, version()), ('fnpackkit', fnpackkit, tool_version())]:
        source = Path(module.__file__).resolve().parent
        result[name] = {'version': release, 'python_source': str(source), 'source_tree_sha256': tree_digest(source)}
        checkout = source.parent.parent
        if (checkout / '.git').exists():
            result[name]['git_commit'] = run(['git', 'rev-parse', 'HEAD'], cwd=checkout, capture=True).strip()
    return result
