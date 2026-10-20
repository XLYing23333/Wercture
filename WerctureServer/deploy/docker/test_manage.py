import os
import json
import pathlib
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

import console


def _database(path):
    path.parent.mkdir(parents=True, exist_ok=True)
    (path.parent / "files").mkdir(exist_ok=True)
    (path.parent / "files" / "picture").write_bytes(b"image")
    connection = sqlite3.connect(path)
    connection.executescript(
        """
        CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT);
        CREATE TABLE warehouses (id INTEGER PRIMARY KEY, name TEXT);
        CREATE TABLE images (id INTEGER PRIMARY KEY);
        CREATE TABLE settings (id INTEGER PRIMARY KEY, locale TEXT, theme TEXT);
        INSERT INTO users(username) VALUES ('admin');
        INSERT INTO warehouses(name) VALUES ('album'), ('work');
        INSERT INTO images DEFAULT VALUES;
        INSERT INTO settings(id, locale, theme) VALUES (1, 'zh-CN', 'dark');
        """
    )
    connection.close()


class StorageTest(unittest.TestCase):
    def test_legacy_cleanup_only_removes_verified_wercture_container(self):
        with tempfile.TemporaryDirectory() as work:
            root = pathlib.Path(work)
            removed = []

            def docker(*args, **kwargs):
                if args[0] == "ps":
                    return console.Result(0, "ours\nforeign\nwrong-image\n")
                if args[0] == "inspect":
                    name = args[1]
                    return console.Result(0, json.dumps([{"Config": {
                        "Image": "unrelated:latest" if name == "wrong-image" else "wercture-server:0.1.0",
                        "Labels": {
                            "com.docker.compose.project": "docker",
                            "com.docker.compose.service": "wercture",
                            "com.docker.compose.project.working_dir": str(root if name != "foreign" else root / "other"),
                        },
                    }}]))
                removed.append(args)
                return console.Result(0, "")

            with patch.object(console, "ROOT", root), patch.object(console, "docker", docker):
                console.remove_legacy_containers()
            self.assertEqual(removed, [("rm", "-f", "ours")])

    def test_resolve_accepts_dedicated_directory_and_rejects_unsafe_paths(self):
        self.assertEqual(console.resolve_storage("/var/lib/wercture"), pathlib.Path("/var/lib/wercture"))
        self.assertEqual(console.resolve_storage(str(console.ROOT / "data")), (console.ROOT / "data").resolve())
        for value in ("relative", "/etc/wercture", "/", str(console.ROOT), str(console.ROOT / "other")):
            with self.assertRaises(console.Failed):
                console.resolve_storage(value)
        with tempfile.TemporaryDirectory() as work:
            root = pathlib.Path(work)
            target = root / "real"
            target.mkdir()
            link = root / "link"
            link.symlink_to(target, target_is_directory=True)
            with self.assertRaises(console.Failed):
                console.resolve_storage(str(link))

    def test_inspect_loads_existing_warehouses(self):
        with tempfile.TemporaryDirectory() as work:
            data = pathlib.Path(work) / "library"
            _database(data / "wercture.db")
            info = console.inspect_storage(data)
            self.assertTrue(info["present"])
            self.assertEqual(info["warehouses"], ["album", "work"])
            self.assertEqual(info["images"], 1)
            self.assertEqual(info["files"], 1)
            self.assertEqual(info["locale"], "zh-CN")
            self.assertIn("album", console.storage_text(info))
            (data / "notes.txt").write_text("nope")
            with self.assertRaises(console.Failed):
                console.inspect_storage(data)

    def test_image_pin_keeps_storage_path(self):
        with tempfile.TemporaryDirectory() as work:
            root = pathlib.Path(work)
            with patch.object(console, "ROOT", root):
                console.write_env({"WERCTURE_IMAGE": "wercture-server:0.1.0", "WERCTURE_DATA": "/var/lib/wercture"})
                console.write_image_ref("wercture-server:0.2.0")
                self.assertEqual(
                    console.env_map(),
                    {"WERCTURE_IMAGE": "wercture-server:0.2.0", "WERCTURE_DATA": "/var/lib/wercture"},
                )
                self.assertEqual(console.storage_path(), pathlib.Path("/var/lib/wercture"))

    def test_install_requires_data_loads_it_and_prunes_after_start(self):
        with tempfile.TemporaryDirectory() as work:
            root = pathlib.Path(work) / "deploy"
            data = pathlib.Path(work) / "library"
            root.mkdir()
            _database(data / "wercture.db")
            order = []

            def compose(*args, **kwargs):
                order.append(("compose", args[0]))
                return console.Result(0, "")

            def docker(*args, **kwargs):
                order.append(("docker", args[:3]))
                return console.Result(0, "")

            with patch.object(console, "ROOT", root), \
                    patch.object(console, "require_root", lambda: None), \
                    patch.object(console, "install_unit", lambda: order.append(("unit",))), \
                    patch.object(console, "wait_health", lambda: order.append(("health",))), \
                    patch.object(console, "compose", compose), \
                    patch.object(console, "docker", docker), \
                    patch.dict(os.environ, {"WERCTURE_IMAGE": ""}, clear=False):
                with self.assertRaises(console.Failed):
                    console.install(None)
                console.install(str(data))
            self.assertTrue((data / "wercture.db").is_file())
            self.assertEqual((data / "files" / "picture").read_bytes(), b"image")
            self.assertIn("WERCTURE_DATA=" + str(data.resolve()), (root / ".env").read_text())
            prune_at = [
                index for index, item in enumerate(order)
                if item == ("docker", ("builder", "prune", "-af"))
            ]
            up_at = order.index(("compose", "up"))
            self.assertGreater(prune_at[-1], up_at)
            self.assertLess(order.index(("health",)), prune_at[-1])

    def test_uninstall_removes_image_and_keeps_storage(self):
        with tempfile.TemporaryDirectory() as work:
            root = pathlib.Path(work) / "deploy"
            data = pathlib.Path(work) / "library"
            root.mkdir()
            _database(data / "wercture.db")
            removed = []

            def docker(*args, **kwargs):
                if args[:2] == ("images", "wercture-server"):
                    return console.Result(0, "sha256:abc\n")
                removed.append(args)
                return console.Result(0, "")

            with patch.object(console, "ROOT", root), \
                    patch.object(console, "require_root", lambda: None), \
                    patch.object(console, "compose", lambda *args, **kwargs: console.Result(0, "")), \
                    patch.object(console, "docker", docker), \
                    patch.object(console, "remove_unit_if_ours", lambda: None):
                console.write_env({"WERCTURE_DATA": str(data)})
                console.uninstall()
                self.assertEqual(console.storage_path(), data)
            self.assertIn(("image", "rm", "sha256:abc"), removed)
            self.assertTrue((data / "wercture.db").is_file())
            self.assertTrue((data / "files" / "picture").is_file())

    def test_cli_install_requires_data_flag(self):
        with patch.object(console, "install", lambda storage: None) as install:
            self.assertEqual(console.dispatch(["install"]), 2)
            self.assertEqual(console.dispatch(["install", "--data"]), 2)
            self.assertEqual(console.dispatch(["install", "--data", "/var/lib/wercture"]), 0)


if __name__ == "__main__":
    unittest.main()
