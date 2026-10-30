#!/usr/bin/env python3
"""Attach the current Actions run's verified outputs to a draft, without replacing assets."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time

VERSION = re.compile(r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$")
FORK_RELEASE = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+-nl\.[1-9][0-9]*$")
TARGETS = ("linux-amd64", "linux-arm64", "macos-amd64", "macos-arm64", "windows-amd64", "windows-arm64")


def gh(*args):
    return subprocess.check_output(["gh", *args], text=True).strip()


def api(path):
    return json.loads(gh("api", path))


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size == 0:
        raise ValueError(f"asset must be a nonempty regular file: {path}")
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def local_assets(version, archives, msi, sbom, d2js=None):
    names = {f"d2-{version}-{target}.tar.gz" for target in TARGETS}
    if archives.is_symlink() or not archives.is_dir():
        raise ValueError("archive directory must be a real directory")
    if {path.name for path in archives.iterdir()} != names | {"SHA256SUMS"}:
        raise ValueError("archive directory must contain exactly six archives and SHA256SUMS")
    manifest = archives / "SHA256SUMS"
    digest(manifest)
    checksums = {}
    for line in manifest.read_text().splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([^/\\]+)", line)
        if not match or match[2] not in names or match[2] in checksums:
            raise ValueError("invalid or duplicate checksum entry")
        checksums[match[2]] = "sha256:" + match[1]
    if checksums.keys() != names:
        raise ValueError("checksum manifest must cover exactly six archives")
    for name, expected in checksums.items():
        if digest(archives / name) != expected:
            raise ValueError(f"archive checksum mismatch: {name}")
    # The MSI needs the owner's WiX EULA acceptance, so a release may go without it.
    if msi is not None and msi.name != f"d2-{version}-windows-amd64.msi":
        raise ValueError("unexpected MSI filename")
    if d2js is not None and d2js.name != d2js_filename(version):
        raise ValueError("unexpected D2.js package filename")
    if sbom.name != "d2.spdx.json":
        raise ValueError("unexpected SBOM filename")
    optional = [path for path in (msi, d2js) if path is not None]
    paths = [archives / name for name in sorted(names)] + [manifest, *optional, sbom]
    return [(path, digest(path), path.stat().st_size) for path in paths]


def d2js_filename(version):
    return f"d2js-{version}.tgz"


def is_prerelease(version):
    # NamiLinkLabs fork releases (v0.10.0-nl.1) are the fork's stable releases.
    return "-" in version and not FORK_RELEASE.fullmatch(version)


def verify_tag(repository, version, commit):
    obj = api(f"repos/{repository}/git/ref/tags/{version}")["object"]
    for _ in range(8):
        if obj["type"] == "commit":
            if obj["sha"] != commit:
                raise ValueError("release tag changed after this run built its artifacts")
            return
        if obj["type"] != "tag":
            break
        obj = api(f"repos/{repository}/git/tags/{obj['sha']}")["object"]
    raise ValueError("release tag does not resolve to the expected commit")


def find_release(repository, version):
    pages = json.loads(gh("api", "--paginate", "--slurp", f"repos/{repository}/releases?per_page=100"))
    matches = [release for page in pages for release in page if release["tag_name"] == version]
    if len(matches) > 1:
        raise ValueError("multiple releases have the requested tag")
    return matches[0] if matches else None


def verify_draft(release, version, release_id=None, prerelease=None):
    if release["tag_name"] != version or release["draft"] is not True:
        raise ValueError("release must remain a draft until all artifacts are reviewed")
    if type(release["id"]) is not int or type(release["prerelease"]) is not bool:
        raise ValueError("invalid draft identity or prerelease state")
    if release_id is not None and (release["id"] != release_id or release["prerelease"] != prerelease):
        raise ValueError("draft identity or prerelease state changed during upload")


def existing_asset(release, path, expected_digest, expected_size, allow_pending=False):
    matches = [asset for asset in release["assets"] if asset["name"] == path.name]
    if not matches:
        return False
    if allow_pending and len(matches) == 1 and (matches[0].get("state") != "uploaded" or not matches[0].get("digest")):
        return False
    if len(matches) != 1 or matches[0].get("state") != "uploaded" or matches[0].get("digest") != expected_digest or matches[0].get("size") != expected_size:
        raise ValueError(f"existing asset has different bytes or incomplete metadata: {path.name}")
    return True


def starter_asset_id(asset):
    if (asset.get("state") == "starter" and type(asset.get("size")) is int and asset["size"] == 0
            and asset.get("digest") is None and type(asset.get("id")) is int and asset["id"] > 0):
        return asset["id"]
    return None


def preflight_assets(release, assets):
    starters = {}
    for asset in assets:
        matches = [item for item in release["assets"] if item["name"] == asset[0].name]
        asset_id = starter_asset_id(matches[0]) if len(matches) == 1 else None
        if asset_id is not None:
            starters[asset[0].name] = asset_id
            continue
        existing_asset(release, *asset)
    return starters


def upload(repository, version, commit, assets):
    verify_tag(repository, version, commit)
    release = find_release(repository, version)
    if release is None:
        notes = Path("ci/release/changelogs") / f"{version}.md"
        if not notes.is_file():
            raise ValueError(f"release changelog is missing: {notes}")
        args = ["release", "create", version, "--repo", repository, "--verify-tag", "--draft", "--title", version, "--notes-file", str(notes)]
        if is_prerelease(version):
            args.append("--prerelease")
        gh(*args)
        # The release list can lag a few seconds behind the create call.
        for attempt in range(10):
            release = find_release(repository, version)
            if release is not None:
                break
            time.sleep(3)
        if release is None:
            raise ValueError("created draft release was not found")
    verify_draft(release, version)
    release_id = release["id"]
    prerelease = release["prerelease"]
    # A failed GitHub upload can leave an empty starter. Never delete completed assets.
    starters = preflight_assets(release, assets)
    for name, asset_id in starters.items():
        release = api(f"repos/{repository}/releases/{release_id}")
        verify_draft(release, version, release_id, prerelease)
        if preflight_assets(release, assets).get(name) != asset_id:
            raise ValueError(f"starter asset changed before cleanup: {name}")
        asset = api(f"repos/{repository}/releases/assets/{asset_id}")
        if asset.get("name") != name or starter_asset_id(asset) != asset_id:
            raise ValueError(f"starter asset changed before cleanup: {name}")
        verify_tag(repository, version, commit)
        gh("api", "--method", "DELETE", f"repos/{repository}/releases/assets/{asset_id}")
    for path, expected_digest, expected_size in assets:
        verify_tag(repository, version, commit)
        release = api(f"repos/{repository}/releases/{release_id}")
        verify_draft(release, version, release_id, prerelease)
        if not existing_asset(release, path, expected_digest, expected_size):
            gh("release", "upload", version, str(path), "--repo", repository)
    for attempt in range(5):
        verify_tag(repository, version, commit)
        release = api(f"repos/{repository}/releases/{release_id}")
        verify_draft(release, version, release_id, prerelease)
        if all(existing_asset(release, *asset, allow_pending=True) for asset in assets):
            break
        if attempt == 4:
            raise ValueError("uploaded assets are missing or their digests are not available")
        time.sleep(2)
    message = f"Verified {len(assets)} assets in draft {release['html_url']}. Review the release PR and publish the draft manually."
    print(message)
    if os.environ.get("GITHUB_STEP_SUMMARY"):
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as summary:
            summary.write(message + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("repository", "version", "commit", "archives", "sbom"):
        parser.add_argument(f"--{name}", required=True)
    parser.add_argument("--msi", help="Windows installer; omitted when the MSI was not built")
    parser.add_argument("--d2js", help="D2.js npm tarball named d2js-<version>.tgz")
    args = parser.parse_args()
    if not VERSION.fullmatch(args.version) or not re.fullmatch(r"[0-9a-f]{40}", args.commit) or not re.fullmatch(r"[\w.-]+/[\w.-]+", args.repository):
        parser.error("invalid repository, version, or commit")
    optional = {name: Path(value) if value else None for name, value in (("msi", args.msi), ("d2js", args.d2js))}
    assets = local_assets(args.version, Path(args.archives), optional["msi"], Path(args.sbom), optional["d2js"])
    upload(args.repository, args.version, args.commit, assets)


if __name__ == "__main__":
    main()
