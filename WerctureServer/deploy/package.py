#!/usr/bin/env python3
"""Build the Wercture Server image and pack the host manager with it."""

import argparse
import hashlib
import json
import os
import pathlib
import shutil
import subprocess
import tarfile
import tempfile

SERVER = pathlib.Path(__file__).resolve().parents[1]
REPO = SERVER.parent
SOURCE = SERVER / "deploy" / "docker"
FILES = (
    "manage",
    "console.py",
    "tui.py",
    "update.py",
    "compose.yaml",
    "wercture.service",
    "BUILD.json",
    "VERSION",
)


def checksum(path):
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def build_image(image, version):
    """Compile on the host, then pack a scratch image with the same entrypoint as the Dockerfile."""
    subprocess.run(["npm", "run", "build"], cwd=SERVER / "web", check=True)
    env = os.environ.copy()
    env["CGO_ENABLED"] = "0"
    with tempfile.TemporaryDirectory(prefix="wercture-binary-") as tmp:
        binary = pathlib.Path(tmp) / "wercture-server"
        subprocess.run(
            [
                "go", "build", "-trimpath", "-buildvcs=false",
                "-ldflags", "-s -w -X main.version=" + version,
                "-o", str(binary), "./cmd/wercture-server",
            ],
            cwd=SERVER,
            env=env,
            check=True,
        )
        stage = pathlib.Path(tmp) / "image"
        stage.mkdir()
        shutil.copy2(binary, stage / "wercture-server")
        (stage / "Dockerfile").write_text(
            "FROM scratch\n"
            "COPY wercture-server /wercture-server\n"
            "USER 10001:10001\n"
            'ENTRYPOINT ["/wercture-server"]\n'
            'CMD ["run", "--data", "/data", "--listen", ":9416"]\n'
        )
        subprocess.run(["docker", "build", "-t", image, str(stage)], check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", default="0.1.0")
    parser.add_argument("--output", default="")
    parser.add_argument("--skip-build", action="store_true")
    args = parser.parse_args()
    version = args.version
    image = "wercture-server:" + version
    output = pathlib.Path(args.output or (REPO / "dist" / "server" / version)).resolve()
    output.mkdir(parents=True, exist_ok=True)
    if not args.skip_build:
        build_image(image, version)
    inspected = json.loads(subprocess.check_output(["docker", "image", "inspect", image], text=True))[0]
    architecture = inspected["Architecture"]
    if architecture not in ("amd64", "arm64"):
        raise SystemExit("unsupported image architecture: " + architecture)
    name = f"wercture-server_{version}_linux_{architecture}"
    with tempfile.TemporaryDirectory(prefix="wercture-package-") as tmp:
        stage = pathlib.Path(tmp)
        for relative in FILES:
            if relative in ("BUILD.json", "VERSION"):
                continue
            target = stage / relative
            shutil.copy2(SOURCE / relative, target)
            if relative == "manage":
                target.chmod(0o755)
        archive = name + ".docker.tar"
        subprocess.run(["docker", "save", "-o", str(stage / archive), image], check=True)
        (stage / "VERSION").write_text(version + "\n")
        build = {
            "version": version,
            "architecture": "linux/" + architecture,
            "image": image,
            "image_archive": archive,
        }
        (stage / "BUILD.json").write_text(json.dumps(build, indent=2) + "\n")
        listed = [archive, *[item for item in FILES if item != "SHA256SUMS"]]
        sums = "".join(
            checksum(stage / item) + "  " + item + "\n"
            for item in sorted(listed)
        )
        (stage / "SHA256SUMS").write_text(sums)
        release = output / (name + ".release.tar.gz")
        pending = release.with_suffix(".tar.gz.new")
        with tarfile.open(pending, "w:gz") as tar:
            paths = sorted(item for item in stage.rglob("*") if item.is_file())
            for path in paths:
                info = tar.gettarinfo(path, arcname=path.relative_to(stage).as_posix())
                info.uid = info.gid = 0
                info.uname = info.gname = "root"
                info.mode = 0o755 if path.name == "manage" else 0o644
                with path.open("rb") as handle:
                    tar.addfile(info, handle)
        pending.replace(release)
        (output / "SHA256SUMS").write_text(checksum(release) + "  " + release.name + "\n")
        print(release)
        print(checksum(release))


if __name__ == "__main__":
    main()
