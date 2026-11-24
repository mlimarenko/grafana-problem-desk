"""Resolve the version used by the installable plugin package."""

import json
import os
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
STABLE = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")
SHA = re.compile(r"[0-9a-f]{40}")


def read_object(path):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate JSON key")
            result[key] = value
        return result

    value = json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique)
    if not isinstance(value, dict):
        raise ValueError("version input must be an object")
    return value


def stable(value):
    if not isinstance(value, str) or not STABLE.fullmatch(value):
        raise ValueError("version must be stable SemVer")
    return value


def package_version(release_preview=False):
    if release_preview:
        selection = read_object(ROOT / ".ci-components/version.json")
        version = stable(selection.get("version"))
        major, minor, patch = version.split(".")
        commit = os.environ.get("CI_COMMIT_SHA", "")
        iid = os.environ.get("CI_PIPELINE_IID", "")
        if not SHA.fullmatch(commit) or not re.fullmatch(r"[1-9][0-9]*", iid):
            raise ValueError("invalid CI source or pipeline identity")
        expected = {
            "core": version, "major": major, "minor": minor, "patch": patch,
            "prerelease": "", "build_metadata": "", "channel": "stable",
            "source": "release", "release_line": f"{major}.{minor}",
            "release_bump": "patch", "git_tag": "v" + version,
            "image_tag": version, "build_id": iid, "git_sha": commit,
            "is_release": False, "is_release_candidate": True, "is_prerelease": False,
        }
        if any(type(selection.get(k)) is not type(v) or selection[k] != v for k, v in expected.items()):
            raise ValueError("release preview does not bind this version, source and pipeline")
        return version
    descriptor_path = ROOT / ".public-release.json"
    if descriptor_path.exists():
        descriptor = read_object(descriptor_path)
        if (set(descriptor) != {"schema", "version", "source_commit", "pipeline_id", "pipeline_iid"}
                or descriptor.get("schema") != "grafana-problem-desk.public-release/v1"
                or not isinstance(descriptor.get("source_commit"), str)
                or not SHA.fullmatch(descriptor["source_commit"])
                or any(not isinstance(descriptor.get(k), str)
                       or not re.fullmatch(r"[1-9][0-9]*", descriptor[k])
                       for k in ("pipeline_id", "pipeline_iid"))):
            raise ValueError("invalid public release descriptor")
        return stable(descriptor.get("version"))
    return stable(read_object(ROOT / "package.json").get("version"))
