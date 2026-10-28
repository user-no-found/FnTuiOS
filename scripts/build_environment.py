"""Adapt application overrides to the shared registered browser environment."""
import os
from buildflow.browser import resolve_browser


def load_browser_environment():
    names = {'playwright': 'FNTUIOS_PLAYWRIGHT', 'chromium': 'FNTUIOS_CHROMIUM'}
    tools = resolve_browser({key: os.environ[name] for key, name in names.items() if name in os.environ})
    settings = {'FNTUIOS_PLAYWRIGHT': str(tools.playwright.parent), 'FNTUIOS_CHROMIUM': str(tools.chromium)}
    os.environ.update({**tools.environment(), **settings})
    return settings
