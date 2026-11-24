#!/usr/bin/env python3
"""Package the installable plugin, preserving its backend executable mode."""

import argparse
import hashlib
import json
from pathlib import Path
import zipfile
from package_version import package_version

parser = argparse.ArgumentParser()
parser.add_argument('--release-preview', action='store_true')
args = parser.parse_args()
root = Path(__file__).resolve().parents[1]
source = json.loads((root / 'package.json').read_text())
source_manifest = json.loads((root / 'src/plugin.json').read_text())
manifest = json.loads((root / 'dist/plugin.json').read_text())
identifier = manifest['id']
version = package_version(release_preview=args.release_preview)
if (source_manifest['id'] != 'mlimarenko-problemdesk-app'
        or identifier != source_manifest['id']
        or source_manifest['info']['version'] != source['version']):
    raise SystemExit('Plugin manifest and package version differ')
if manifest != source_manifest:
    raise SystemExit('Built plugin manifest differs from source')
binary = root / 'dist/gpx_problemdesk_linux_amd64'
header = binary.open('rb').read(20) if binary.is_file() else b''
if header[:6] != b'\x7fELF\x02\x01' or header[18:20] != b'\x3e\x00' or not binary.stat().st_mode & 0o111:
    raise SystemExit('The Linux amd64 backend executable is missing or invalid')
manifest['info']['version'] = version
destination = root / 'artifacts'
destination.mkdir(exist_ok=True)
archive_path = destination / f'{identifier}-{version}.zip'
with zipfile.ZipFile(archive_path, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
    for path in sorted((root / 'dist').rglob('*')):
        if path.is_file():
            name = f'{identifier}/{path.relative_to(root / "dist")}'
            if path == root / 'dist/plugin.json':
                entry = zipfile.ZipInfo.from_file(path, name)
                archive.writestr(entry, json.dumps(manifest, indent=2) + '\n', compress_type=zipfile.ZIP_DEFLATED)
            else:
                archive.write(path, name)
checksum = hashlib.sha256(archive_path.read_bytes()).hexdigest()
archive_path.with_suffix('.zip.sha256').write_text(f'{checksum}  {archive_path.name}\n')
print(archive_path.relative_to(root))
