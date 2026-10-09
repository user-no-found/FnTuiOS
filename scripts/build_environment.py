"""Load application-local native browser settings without overriding explicit env."""
import json
import os
from pathlib import Path
import subprocess


KEYS = ('FNTUIOS_PLAYWRIGHT', 'FNTUIOS_CHROMIUM')


def read_settings(root):
    path = Path(root) / '.build/environment.json'
    if not path.exists():
        return {}
    try:
        data = json.loads(path.read_text())
    except (OSError, ValueError) as error:
        raise ValueError(f'{path}: invalid local build configuration: {error}') from error
    if not isinstance(data, dict) or type(data.get('schema')) is not int or data.get('schema') != 1 or set(data) != {'schema', 'browser'}:
        raise ValueError(f'{path}: expected schema 1 and browser settings; rerun setup.sh')
    browser = data['browser']
    if not isinstance(browser, dict) or set(browser) != set(KEYS):
        raise ValueError(f'{path}: expected Playwright and Chromium paths; rerun setup.sh')
    for key, value in browser.items():
        if not isinstance(value, str) or not value or not Path(value).is_absolute():
            raise ValueError(f'{path}: {key} must be an absolute path; rerun setup.sh')
    return browser


def validate_browser(settings):
    """Check existing dependencies; registration never installs software."""
    for key in KEYS:
        value = settings.get(key)
        if not isinstance(value, str) or not value or not Path(value).is_absolute():
            raise ValueError(f'{key} must name an existing absolute path; run ./setup.sh --help')
    playwright, chromium = (Path(settings[key]) for key in KEYS)
    try:
        package = json.loads((playwright / 'package.json').read_text())
    except (OSError, ValueError) as error:
        raise ValueError(f'Invalid Playwright package: {playwright}') from error
    if not isinstance(package, dict) or package.get('name') != 'playwright':
        raise ValueError(f'Expected the playwright package directory: {playwright}')
    if not chromium.is_file() or not os.access(chromium, os.X_OK):
        raise ValueError(f'Chromium must be an executable file: {chromium}')
    commands = [
        ['node', '-e', 'const p=require(process.argv[1]);if(!p.chromium || typeof p.chromium.launch!=="function")process.exit(1)', str(playwright)],
        [str(chromium), '--version'],
    ]
    for command in commands:
        try:
            result = subprocess.run(command, check=True, capture_output=True, text=True, timeout=15)
        except (OSError, subprocess.SubprocessError) as error:
            raise ValueError(f'Browser dependency check failed: {command[0]}: {error}') from error
        if command[0] == str(chromium) and not any(name in result.stdout.lower() for name in ['chromium', 'chrome']):
            raise ValueError(f'Executable did not report Chrome/Chromium: {chromium}')


def load_browser_environment(root):
    """Resolve once at plan selection, with explicit environment taking precedence."""
    configured = read_settings(root)
    settings = {key: os.environ[key] if key in os.environ else configured.get(key) for key in KEYS}
    validate_browser(settings)
    for key, value in settings.items():
        os.environ.setdefault(key, value)
    return settings
