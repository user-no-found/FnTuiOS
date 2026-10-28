#!/usr/bin/env python3
"""Register existing browser test tools through BuildFlow's shared interface."""
import argparse
import shutil
from buildflow.browser import register_browser
from buildflow.contracts import BuildError


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('node', 'playwright', 'chromium'):
        parser.add_argument('--' + name, help='existing absolute tool path')
    args = parser.parse_args()
    supplied = {key: value for key, value in vars(args).items() if value is not None}
    if 'node' not in supplied and ('playwright' in supplied or 'chromium' in supplied):
        node = shutil.which('node')
        if node:
            supplied['node'] = node
    try:
        tools = register_browser(supplied)
    except BuildError as error:
        parser.exit(1, f'FnTuiOS setup: {error}\n')
    print('Shared browser test tools: ' + ', '.join(tools.versions.values()))


if __name__ == '__main__':
    main()
