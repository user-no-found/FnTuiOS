#!/usr/bin/env python3
"""Check a fetched source tree against reviewed localization data."""
import argparse
import collections
import json
from pathlib import Path
import sys
import subprocess

from build_inputs import ROOT
import localization


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--sip', type=Path, required=True)
    parser.add_argument('--go', type=Path, required=True)
    parser.add_argument('--output', type=Path, default=ROOT / 'dist/localization')
    parser.add_argument('--refresh-uncovered', action='store_true', help='extract and report missing memory entries without accepting inventory')
    parser.add_argument('--approve-inventory', action='store_true', help='explicitly record a fully classified, reviewed source inventory')
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    if args.refresh_uncovered:
        candidates = localization.extract(args.go.resolve(), args.source.resolve(), args.sip.resolve(), args.output / 'candidates.json')
        reviewed, missing = localization.reconcile(candidates, localization.memory())
        groups = collections.defaultdict(list)
        for entry in missing:
            groups[entry['source']].append(entry)
        sources = [{'source': source, 'occurrences': len(entries), 'contexts': [{'file': e['file'], 'context': e['context']} for e in entries[:5]]} for source, entries in groups.items()]
        localization.write_json(args.output / 'unclassified.json', missing)
        localization.write_json(args.output / 'uncovered-sources.json', sources)
        print(f'{len(reviewed)} reviewed, {len(missing)} missing contexts, {len(sources)} unique missing sources')
        return 0
    try:
        localization.gate(args.go.resolve(), args.source.resolve(), args.sip.resolve(), args.output.resolve(), args.approve_inventory)
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f'Localization gate failed: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
