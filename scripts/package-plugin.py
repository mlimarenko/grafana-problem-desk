#!/usr/bin/env python3
"""Package the installable plugin, preserving its backend executable mode."""

import hashlib
import json
from pathlib import Path
import zipfile

root = Path(__file__).resolve().parents[1]
source = json.loads((root / 'package.json').read_text())
manifest = json.loads((root / 'dist/plugin.json').read_text())
identifier = manifest['id']
version = source['version']
if manifest['info']['version'] != version:
    raise SystemExit('Plugin manifest and package version differ')
binary = root / 'dist/gpx_problemdesk_linux_amd64'
header = binary.open('rb').read(20) if binary.is_file() else b''
if header[:6] != b'\x7fELF\x02\x01' or header[18:20] != b'\x3e\x00' or not binary.stat().st_mode & 0o111:
    raise SystemExit('The Linux amd64 backend executable is missing or invalid')
destination = root / 'artifacts'
destination.mkdir(exist_ok=True)
archive_path = destination / f'{identifier}-{version}.zip'
with zipfile.ZipFile(archive_path, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
    for path in sorted((root / 'dist').rglob('*')):
        if path.is_file():
            archive.write(path, f'{identifier}/{path.relative_to(root / "dist")}')
checksum = hashlib.sha256(archive_path.read_bytes()).hexdigest()
archive_path.with_suffix('.zip.sha256').write_text(f'{checksum}  {archive_path.name}\n')
print(archive_path.relative_to(root))
