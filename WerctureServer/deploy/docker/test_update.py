import hashlib
import io
import json
import pathlib
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import console
import update


def _sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _stage_release(root, version="1.2.3", version_text="wercture-server 1.2.3\n"):
    stage = root / "stage"
    stage.mkdir()
    archive = "wercture-server_" + version + "_linux_amd64.docker.tar"
    files = {
        "manage": "#!/bin/sh\n",
        "console.py": "new console\n",
        "tui.py": "new tui\n",
        "update.py": "new update\n",
        "compose.yaml": "name: wercture\n",
        "wercture.service": "[Service]\n",
        "BUILD.json": json.dumps({
            "version": version,
            "architecture": "linux/amd64",
            "image": "wercture-server:" + version,
            "image_archive": archive,
        }),
        "VERSION": version + "\n",
        archive: "image-bytes",
    }
    for name, text in files.items():
        (stage / name).write_text(text)
    names = sorted(files)
    lines = [_sha256(stage / name) + "  " + name + "\n" for name in names]
    (stage / "SHA256SUMS").write_text("".join(lines))
    packed = root / "release.tar.gz"
    with tarfile.open(packed, "w:gz") as tar:
        for name in ["SHA256SUMS", *names]:
            tar.add(stage / name, arcname=name)
    return {
        "tag_name": "v" + version,
        "archive": archive,
        "version_text": version_text,
    }


class UpdateTest(unittest.TestCase):
    def test_successful_update_keeps_verified_bundle_for_reinstall(self):
        with tempfile.TemporaryDirectory() as work, patch("update.platform.machine", return_value="x86_64"):
            root = pathlib.Path(work)
            packed = _stage_release(root)
            with patch.object(console, "ROOT", root), \
                    patch.object(console, "require_root", lambda: None), \
                    patch.object(console, "docker", lambda *args, **kwargs: console.Result(0, "")), \
                    patch.object(console, "compose", lambda *args, **kwargs: None), \
                    patch.object(console, "wait_health", lambda: None), \
                    patch.object(console, "server_cmd", lambda *args, **kwargs: console.Result(0, packed["version_text"])), \
                    patch("update.download"), \
                    patch("update.extract", side_effect=lambda source, target: _extract_fixture(root, target)):
                update.apply({"tag_name": packed["tag_name"]})
                image, archive = console.bundled_image()
                self.assertEqual(image, "wercture-server:1.2.3")
                self.assertEqual(archive.read_bytes(), b"image-bytes")
                update.verify(root, {"tag_name": packed["tag_name"]})
                self.assertFalse((root / ".update-previous").exists())

    def test_versions_and_exact_assets(self):
        self.assertGreater(update.version("v0.10.0"), update.version("0.9.9"))
        for value in ["0.5", "v01.2.3", "0.5.0-rc1"]:
            with self.assertRaises(RuntimeError):
                update.version(value)
        release = {
            "tag_name": "v1.2.3",
            "assets": [{
                "name": "x",
                "size": 1,
                "browser_download_url": update.PREFIX + "v1.2.3/x",
            }],
        }
        update.asset(release, "x")
        release["assets"][0]["browser_download_url"] = "https://example.com/x"
        with self.assertRaises(RuntimeError):
            update.asset(release, "x")

    def test_check_rejects_draft_prerelease_and_wrong_url(self):
        def payload(**extra):
            body = {
                "tag_name": "v1.2.3",
                "draft": False,
                "prerelease": False,
                "html_url": "https://github.com/XLYing23333/Wercture/releases/tag/v1.2.3",
            }
            body.update(extra)
            return json.dumps(body).encode(), "abc"

        with patch("update.read", return_value=payload()):
            self.assertEqual(update.check()["tag_name"], "v1.2.3")
        for extra in (
            {"prerelease": True},
            {"draft": True},
            {"html_url": "https://github.com/XLYing23333/Weroxy/releases/tag/v1.2.3"},
        ):
            with patch("update.read", return_value=payload(**extra)):
                with self.assertRaises(RuntimeError):
                    update.check()

    def test_extract_rejects_links_traversal_and_data_paths(self):
        for name, kind in [
            ("../escape", tarfile.REGTYPE),
            ("link", tarfile.SYMTYPE),
            ("/absolute", tarfile.REGTYPE),
            ("data/wercture.db", tarfile.REGTYPE),
        ]:
            with tempfile.TemporaryDirectory() as work:
                root = pathlib.Path(work)
                source = root / "a.tar.gz"
                target = root / "out"
                target.mkdir()
                with tarfile.open(source, "w:gz") as tar:
                    info = tarfile.TarInfo(name)
                    info.type = kind
                    info.linkname = "/etc/passwd"
                    info.size = 0
                    tar.addfile(info, io.BytesIO(b""))
                with self.assertRaises(RuntimeError):
                    update.extract(source, target)
                self.assertEqual(list(target.iterdir()), [])

    def test_bad_checksum_stops_before_install(self):
        release = {"tag_name": "v1.2.3", "assets": []}
        name = "wercture-server_1.2.3_linux_amd64.release.tar.gz"
        for asset_name, size in [("SHA256SUMS", 80), (name, 3)]:
            release["assets"].append({
                "name": asset_name,
                "size": size,
                "browser_download_url": update.PREFIX + "v1.2.3/" + asset_name,
            })

        def read(url, limit, output=None, timeout=10, cancel=None):
            if output is None:
                return (("0" * 64 + "  " + name + "\n").encode(), "")
            output.write(b"bad")
            return 3, "1" * 64

        with tempfile.TemporaryDirectory() as work, patch("update.platform.machine", return_value="x86_64"), patch("update.read", side_effect=read):
            with self.assertRaisesRegex(RuntimeError, "SHA-256"):
                update.download(release, pathlib.Path(work) / "package")

    def test_failed_switch_restores_manager_and_keeps_data(self):
        with tempfile.TemporaryDirectory() as work, patch("update.platform.machine", return_value="x86_64"):
            root = pathlib.Path(work)
            (root / "console.py").write_text("old console\n")
            data = root / "data" / "files"
            data.mkdir(parents=True)
            marker = data / "keep"
            marker.write_bytes(b"picture")
            packed = _stage_release(root)
            calls = []

            def compose(*args, **kwargs):
                calls.append((args, kwargs.get("image")))
                if len(calls) == 1:
                    raise console.Failed("health failed")

            with patch.dict("os.environ", {"WERCTURE_IMAGE": ""}), \
                    patch.object(console, "ROOT", root), \
                    patch.object(console, "require_root", lambda: None), \
                    patch.object(console, "docker", lambda *args, **kwargs: console.Result(0, "")), \
                    patch.object(console, "compose", compose), \
                    patch.object(console, "wait_health", lambda: None), \
                    patch("update.download"), \
                    patch("update.extract", side_effect=lambda source, target: _extract_fixture(root, target)):
                with self.assertRaises(console.Failed):
                    update.apply({"tag_name": packed["tag_name"]})
            self.assertEqual((root / "console.py").read_text(), "old console\n")
            self.assertEqual(marker.read_bytes(), b"picture")
            self.assertFalse((root / ".update-previous").exists())
            self.assertFalse((root / "data" / "wercture.db").exists())
            self.assertIn("--no-build", calls[0][0])
            self.assertNotIn("-v", calls[0][0])

    def test_version_mismatch_rolls_container_back(self):
        with tempfile.TemporaryDirectory() as work, patch("update.platform.machine", return_value="x86_64"):
            root = pathlib.Path(work)
            (root / "console.py").write_text("old console\n")
            (root / ".env").write_text("WERCTURE_IMAGE=wercture-server:1.0.0\n")
            packed = _stage_release(root, version_text="wercture-server 0.0.0\n")
            images = []

            def compose(*args, **kwargs):
                images.append(kwargs.get("image"))

            with patch.dict("os.environ", {"WERCTURE_IMAGE": ""}), \
                    patch.object(console, "ROOT", root), \
                    patch.object(console, "require_root", lambda: None), \
                    patch.object(console, "docker", lambda *args, **kwargs: console.Result(0, "")), \
                    patch.object(console, "compose", compose), \
                    patch.object(console, "wait_health", lambda: None), \
                    patch.object(console, "server_cmd", lambda *args, **kwargs: console.Result(0, packed["version_text"])), \
                    patch("update.download"), \
                    patch("update.extract", side_effect=lambda source, target: _extract_fixture(root, target)):
                with self.assertRaisesRegex(RuntimeError, "version check failed"):
                    update.apply({"tag_name": packed["tag_name"]})
            self.assertEqual((root / "console.py").read_text(), "old console\n")
            self.assertEqual((root / ".env").read_text(), "WERCTURE_IMAGE=wercture-server:1.0.0\n")
            self.assertEqual(images, ["wercture-server:1.2.3", "wercture-server:1.0.0"])

    def test_metadata_time_budget_and_archive_entry_budget(self):
        response = type("Response", (), {"read1": lambda self, n: b""})()

        class Opened:
            def __enter__(self):
                return response

            def __exit__(self, *args):
                return False

        opener = type("Opener", (), {"open": lambda self, *args, **kwargs: Opened()})()
        with patch("update.urllib.request.build_opener", return_value=opener), \
                patch("update.time.monotonic", side_effect=[0, 11]):
            with self.assertRaisesRegex(RuntimeError, "time budget"):
                update.read(update.API, 1024)
        with tempfile.TemporaryDirectory() as work:
            root = pathlib.Path(work)
            source = root / "many.tar.gz"
            target = root / "out"
            target.mkdir()
            with tarfile.open(source, "w:gz") as archive:
                for index in range(101):
                    info = tarfile.TarInfo(str(index))
                    info.size = 0
                    archive.addfile(info, io.BytesIO(b""))
            with self.assertRaisesRegex(RuntimeError, "budget"):
                update.extract(source, target)
            self.assertEqual(list(target.iterdir()), [])


def _extract_fixture(root, target):
    stage = root / "stage"
    for path in stage.iterdir():
        if path.is_file():
            dest = target / path.name
            shutil_copy(path, dest)


def shutil_copy(source, dest):
    dest.write_bytes(source.read_bytes())


if __name__ == "__main__":
    unittest.main()
