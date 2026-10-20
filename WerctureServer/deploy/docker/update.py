"""Download a verified GitHub release and switch this deployment to it.

The stable release is published at
https://github.com/XLYing23333/Wercture/releases
as wercture-server_<version>_linux_<arch>.release.tar.gz plus SHA256SUMS.
The package carries the manager files and one docker image archive.
data/ is never replaced.
"""

import fcntl
import hashlib
import json
import os
import pathlib
import platform
import re
import shutil
import sys
import tarfile
import tempfile
import time
import urllib.parse
import urllib.request

API = "https://api.github.com/repos/XLYing23333/Wercture/releases/latest"
PREFIX = "https://github.com/XLYing23333/Wercture/releases/download/"
HOSTS = {
    "github.com",
    "api.github.com",
    "release-assets.githubusercontent.com",
    "objects.githubusercontent.com",
}
MANAGER_FILES = (
    "manage",
    "console.py",
    "tui.py",
    "update.py",
    "compose.yaml",
    "wercture.service",
    "BUILD.json",
    "VERSION",
)


def version(value):
    if not re.fullmatch(r"v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", value):
        raise RuntimeError("Invalid stable release version")
    return tuple(int(part) for part in value.removeprefix("v").split("."))


class Redirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        target = urllib.parse.urlsplit(newurl)
        if target.scheme != "https" or target.username or target.hostname not in HOSTS:
            raise RuntimeError("Unsafe release redirect")
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def read(url, limit, output=None, timeout=10, cancel=None):
    request = urllib.request.Request(url, headers={"User-Agent": "Wercture-update"})
    started = time.monotonic()
    budget = 10 if output is None else 600
    with urllib.request.build_opener(Redirect()).open(request, timeout=timeout) as response:
        chunks = []
        size = 0
        digest = hashlib.sha256()
        while True:
            if time.monotonic() - started >= budget:
                raise RuntimeError("Release request exceeds time budget")
            if cancel is not None and cancel.is_set():
                raise RuntimeError("Update cancelled before installation")
            block = response.read1(65536)
            if not block:
                break
            size += len(block)
            if size > limit or time.monotonic() - started >= budget:
                raise RuntimeError("Release download exceeds budget")
            digest.update(block)
            if output is None:
                chunks.append(block)
            else:
                output.write(block)
        return (b"".join(chunks) if output is None else size), digest.hexdigest()


def check():
    raw, _ = read(API, 2 * 1024 ** 2)
    result = json.loads(raw)
    version(result["tag_name"])
    expected = "https://github.com/XLYing23333/Wercture/releases/tag/" + result["tag_name"]
    if result.get("draft") or result.get("prerelease") or result.get("html_url") != expected:
        raise RuntimeError("Invalid stable release")
    return result


def asset(release, name):
    matches = [item for item in release["assets"] if item["name"] == name]
    expected = PREFIX + release["tag_name"] + "/" + name
    if len(matches) != 1 or matches[0]["browser_download_url"] != expected or not 0 < matches[0]["size"]:
        raise RuntimeError("Required release asset missing or invalid: " + name)
    return matches[0]


def candidate_name(release):
    arch = {"x86_64": "amd64", "aarch64": "arm64"}.get(platform.machine())
    if arch is None:
        raise RuntimeError("No Server update package for this architecture")
    return "wercture-server_" + release["tag_name"].removeprefix("v") + "_linux_" + arch + ".release.tar.gz"


def download(release, target, cancel=None):
    name = candidate_name(release)
    manifest = asset(release, "SHA256SUMS")
    if manifest["size"] > 1024 ** 2:
        raise RuntimeError("Checksum manifest too large")
    raw, _ = read(manifest["browser_download_url"], 1024 ** 2)
    hashes = {}
    for line in raw.decode().splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([^\r\n]+)", line)
        if not match or match[2] in hashes:
            raise RuntimeError("Invalid checksum manifest")
        hashes[match[2]] = match[1]
    item = asset(release, name)
    if item["size"] > 1024 ** 3 or name not in hashes:
        raise RuntimeError("Invalid Server asset size or checksum")
    with target.open("xb") as stream:
        size, digest = read(item["browser_download_url"], 1024 ** 3, stream, timeout=30, cancel=cancel)
        stream.flush()
        os.fsync(stream.fileno())
    if digest != hashes[name] or size != item["size"]:
        raise RuntimeError("Server release SHA-256 or size mismatch")


def extract(source, target):
    with tarfile.open(source, "r:gz") as archive:
        members = []
        expanded = 0
        for member in archive:
            expanded += member.size
            if len(members) >= 100 or expanded > 2 * 1024 ** 3:
                raise RuntimeError("Expanded release exceeds budget")
            members.append(member)
        names = set()
        for member in members:
            parts = pathlib.PurePosixPath(member.name)
            if (
                parts.is_absolute()
                or len(parts.parts) != 1
                or member.name in names
                or not member.isfile()
            ):
                raise RuntimeError("Unsafe release archive entry")
            names.add(member.name)
        for member in members:
            dest = target / member.name
            with archive.extractfile(member) as source_file, dest.open("xb") as output:
                shutil.copyfileobj(source_file, output)
            dest.chmod(0o755 if member.name == "manage" else 0o644)


def verify(stage, release):
    manifest = stage / "SHA256SUMS"
    if not manifest.is_file() or manifest.is_symlink() or manifest.stat().st_size > 65536:
        raise RuntimeError("Invalid release manifest")
    names = set()
    for row in manifest.read_text().splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([^\r\n]+)", row)
        if not match or match[2] in names or "/" in match[2] or match[2] in ("", ".", ".."):
            raise RuntimeError("Invalid release manifest")
        path = stage / match[2]
        if path.is_symlink() or not path.is_file():
            raise RuntimeError("SHA-256 mismatch: " + match[2])
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        if digest != match[1]:
            raise RuntimeError("SHA-256 mismatch: " + match[2])
        names.add(match[2])
    build = json.loads((stage / "BUILD.json").read_text())
    if version(str(build.get("version", ""))) != version(release["tag_name"]):
        raise RuntimeError("Release package version mismatch")
    arch = {"x86_64": "amd64", "aarch64": "arm64"}.get(platform.machine())
    if build.get("architecture") != "linux/" + str(arch):
        raise RuntimeError("Release architecture differs from host")
    image = build.get("image", "")
    if image != "wercture-server:" + build["version"]:
        raise RuntimeError("Invalid image reference")
    archive = build.get("image_archive", "")
    expected = "wercture-server_" + build["version"] + "_linux_" + str(arch) + ".docker.tar"
    if archive != expected:
        raise RuntimeError("Image missing from release manifest")
    allowed = set(MANAGER_FILES) | {"SHA256SUMS", archive}
    if not set(MANAGER_FILES) <= names or archive not in names or not names <= allowed:
        raise RuntimeError("Incomplete release manifest")
    return build


def _restore(previous, absent):
    import console

    names = set(MANAGER_FILES) | set(absent) | {path.name for path in previous.iterdir()}
    for name in names:
        old = previous / name
        dest = console.ROOT / name
        if old.is_file():
            shutil.copy2(old, dest)
        elif name in absent and dest.is_file() and not dest.is_symlink():
            dest.unlink()


def apply(release, cancel=None):
    import console

    console.require_root()
    previous = console.ROOT / ".update-previous"
    if previous.exists():
        raise RuntimeError("Archive .update-previous before another update")
    lock_path = console.ROOT / ".manage.lock"
    with lock_path.open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError("Another management operation is running") from None
        with tempfile.TemporaryDirectory(prefix=".update-", dir=console.ROOT) as work:
            work = pathlib.Path(work)
            archive = work / "release.tar.gz"
            download(release, archive, cancel)
            stage = work / "release"
            stage.mkdir()
            extract(archive, stage)
            build = verify(stage, release)
            if cancel is not None and cancel.is_set():
                raise RuntimeError("Update cancelled before installation")
            print("Import container image " + build["image"])
            console.docker("load", "-i", str(stage / build["image_archive"]))
            console.docker("image", "inspect", build["image"], quiet=True)
            previous_image = console.image_ref()
            env_path = console.ROOT / ".env"
            try:
                old_env = env_path.read_text() if env_path.is_file() else None
            except OSError as exc:
                raise RuntimeError("Cannot read image pin") from exc
            previous.mkdir(mode=0o700)
            absent = []
            switched = False
            try:
                for name in (*MANAGER_FILES, build["image_archive"], "SHA256SUMS"):
                    dest = console.ROOT / name
                    if dest.is_symlink():
                        raise RuntimeError("Refusing to replace a symlink: " + name)
                    if dest.exists():
                        shutil.copy2(dest, previous / name)
                    else:
                        absent.append(name)
                    pending = console.ROOT / (name + ".update-new")
                    shutil.copyfile(stage / name, pending)
                    pending.chmod(0o755 if name == "manage" else 0o644)
                    pending.replace(dest)
                console.write_image_ref(build["image"])
                switched = True
                console.compose(
                    "up", "-d", "--no-build", "--force-recreate", "--remove-orphans",
                    image=build["image"],
                )
                console.wait_health()
                reported = console.server_cmd("version")
                if reported.stdout.splitlines()[:1] != ["wercture-server " + build["version"]]:
                    raise RuntimeError("Post-update Server version check failed")
                shutil.rmtree(previous)
                print(console.say("update_done"))
                print(build["version"])
                print(f"data: {console.storage_path()}")
            except BaseException:
                _restore(previous, absent)
                if old_env is None:
                    env_path.unlink(missing_ok=True)
                else:
                    env_path.write_text(old_env)
                    os.chmod(env_path, 0o600)
                if switched:
                    try:
                        console.compose(
                            "up", "-d", "--no-build", "--force-recreate", "--remove-orphans",
                            image=previous_image,
                        )
                    except Exception as error:
                        raise RuntimeError(
                            "Update failed and rollback failed; retain .update-previous"
                        ) from error
                shutil.rmtree(previous, ignore_errors=True)
                raise


def execute(confirmed=False, cancel=None):
    import console

    release = check()
    current = console.current_version()
    print("Current: " + current)
    print("Latest: " + release["tag_name"])
    print(release["html_url"])
    if version(release["tag_name"]) <= version(current):
        print(console.say("update_none"))
        return
    asset(release, candidate_name(release))
    if not confirmed:
        if not sys.stdin.isatty():
            raise RuntimeError("Re-run manage update --yes to update without a prompt")
        answer = input(console.say("update_cli") + " ").strip().lower()
        if answer != "y":
            print(console.say("cancelled"))
            return
    apply(release, cancel)
