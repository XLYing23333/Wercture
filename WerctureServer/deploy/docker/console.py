#!/usr/bin/env python3
"""Actions for the Wercture host menu. The curses screens live in tui.py."""

import getpass
import json
import os
import pathlib
import re
import shutil
import sqlite3
import subprocess
import sys
import time
import urllib.error
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent
UNIT = "wercture.service"
UNIT_PATH = pathlib.Path("/etc/systemd/system") / UNIT
DEFAULT_IMAGE = "wercture-server:0.1.0"
DATA_UID = 10001
ALLOWED_NAMES = {
    "wercture.db",
    "wercture.db-wal",
    "wercture.db-shm",
    "wercture.db-journal",
    "files",
    "setup.secret",
}
BLOCKED_STORAGE = {
    pathlib.Path("/bin"),
    pathlib.Path("/boot"),
    pathlib.Path("/dev"),
    pathlib.Path("/etc"),
    pathlib.Path("/lib"),
    pathlib.Path("/lib64"),
    pathlib.Path("/proc"),
    pathlib.Path("/run"),
    pathlib.Path("/sbin"),
    pathlib.Path("/sys"),
    pathlib.Path("/usr"),
}
LEGACY_PROJECT = "docker"
HEALTH_URL = "http://127.0.0.1:9416/health"

TEXT = {
    "en": {
        "title": "Wercture",
        "install": "Install",
        "start": "Start",
        "restart": "Restart",
        "status": "Status",
        "enable": "Enable on boot",
        "disable": "Disable on boot",
        "password": "Reset admin password",
        "language": "Language",
        "english": "EN",
        "chinese": "ZH-CN",
        "uninstall": "Uninstall",
        "back": "Back",
        "config": "Config",
        "hint": "Up/Down: select   Enter: run   R: refresh   Esc/q: back",
        "done": "Finished",
        "need_root": "This action needs root.",
        "new_password": "New admin password (6 to 1024 bytes). The username stays unchanged.",
        "confirm_password": "Repeat the new password",
        "mismatch": "The passwords did not match.",
        "password_length": "Password must be 6 to 1024 bytes.",
        "bad_language": "Language must be en or zh-CN.",
        "data_kept": "Container, boot unit, and wercture-server image were removed. Warehouses and settings were kept.",
        "unit_kept": "Boot unit belongs to another deployment and was left in place.",
        "boot_on": "Boot start is enabled. The container was not started or stopped.",
        "boot_off": "Boot start is disabled. The container was not started or stopped.",
        "cancelled": "Cancelled.",
        "uninstall_prompt": "Remove this deployment's container, boot unit, and wercture-server image. Warehouses and settings stay in {path}. Type UNINSTALL to continue.",
        "storage_prompt": "Host directory for the database, settings, and files. An existing Wercture directory is loaded as-is.",
        "storage_empty": "{path}\nNo Wercture database here yet. A new one will be created.",
        "storage_files_only": "{path}\nNo database yet. {files} existing files will stay, and a new database will be created.",
        "storage_found": "{path}\nExisting data will be loaded: {warehouses} warehouses ({names}), {images} images, {files} files, {users} users, {locale} / {theme}.",
        "storage_loaded": "Existing warehouses and settings were loaded.",
        "install_prompt": "Type INSTALL to continue.",
        "need_storage": "Install needs a storage directory: manage install --data PATH",
        "storage_invalid": "Use an absolute directory that does not contain this deployment.",
        "storage_inside": "Inside this deployment, the only storage directory is data/.",
        "storage_symlink": "The storage path cannot be a symbolic link.",
        "storage_not_dir": "The storage path must be a directory.",
        "storage_parent": "The parent of the storage directory must already exist.",
        "storage_unknown": "This directory is not Wercture storage. Unexpected entries: {names}",
        "storage_unreadable": "The existing database could not be read.",
        "storage_new": "No database yet",
        "label_storage": "storage",
        "label_boot": "boot",
        "label_container": "container",
        "container_running": "running",
        "container_stopped": "stopped",
        "container_unknown": "unknown",
        "step_data": "Preparing the host data directory.",
        "step_down": "Stopping and removing this deployment's containers.",
        "step_image": "Removing the wercture-server image.",
        "step_cache": "Clearing the Docker build cache.",
        "step_load": "Loading the bundled image.",
        "step_build": "Building the image without cache.",
        "step_up": "Starting the container.",
        "step_unit": "Writing the boot unit without changing whether it is enabled.",
        "desc_install": "Choose a storage directory, load it when it already has warehouses, rebuild, and start. Build cache is cleared afterwards.",
        "desc_start": "Start the current container. The boot setting stays unchanged.",
        "desc_restart": "Restart the current container. The boot setting stays unchanged.",
        "desc_status": "Show the container, boot setting, language, and data directory.",
        "desc_enable": "Turn on boot start only. Leave the container running or stopped as it is.",
        "desc_disable": "Turn off boot start only. Leave the container running or stopped as it is.",
        "desc_config": "Reset the admin password, or choose EN or ZH-CN.",
        "update": "Update",
        "desc_update": "Check GitHub for a stable release, verify it, and replace the image and manager. Data is kept.",
        "update_prompt": "Replace this deployment's image and manager. Data is kept. Type UPDATE to continue.",
        "update_none": "No newer stable release.",
        "update_cli": "Replace the image and manager. Data is kept. Continue? [y/N]",
        "update_done": "Updated. Reopen manage to use the new manager. Data was kept.",
        "desc_uninstall": "Remove this deployment's container, boot unit, and Docker image. Keep warehouses and settings.",
        "desc_password": "Set a new admin password. The username is not changed.",
        "desc_language": "Choose the site language, EN or ZH-CN.",
    },
    "zh-CN": {
        "title": "Wercture",
        "install": "安装",
        "start": "启动",
        "restart": "重启",
        "status": "状态",
        "enable": "开机自启",
        "disable": "取消开机自启",
        "password": "重置管理员密码",
        "language": "语言",
        "english": "EN",
        "chinese": "ZH-CN",
        "uninstall": "卸载",
        "back": "返回",
        "config": "配置",
        "hint": "方向键选择，Enter 执行，R 刷新，Esc/q 返回",
        "done": "完成",
        "need_root": "这个操作需要 root。",
        "new_password": "新的管理员密码（6 到 1024 字节）。用户名不变。",
        "confirm_password": "再输入一次新密码",
        "mismatch": "两次密码不一致。",
        "password_length": "密码必须是 6 到 1024 字节。",
        "bad_language": "语言只能是 en 或 zh-CN。",
        "data_kept": "已删除容器、开机项和 wercture-server 镜像。仓库和配置仍保留。",
        "unit_kept": "开机项属于其他部署，已保留。",
        "boot_on": "已开启开机自启。容器保持原样，没有启动或停止。",
        "boot_off": "已关闭开机自启。容器保持原样，没有启动或停止。",
        "cancelled": "已取消。",
        "uninstall_prompt": "将删除本部署的容器、开机项和 wercture-server 镜像。仓库和配置仍留在 {path}。输入 UNINSTALL 继续。",
        "storage_prompt": "数据库、设置和文件所在的宿主机目录。如果这里已经有 Wercture 数据，会原样加载。",
        "storage_empty": "{path}\n这里还没有 Wercture 数据库，将新建一个。",
        "storage_files_only": "{path}\n还没有数据库。已有的 {files} 个文件会保留，并新建数据库。",
        "storage_found": "{path}\n将加载已有数据：{warehouses} 个仓库（{names}），{images} 张图片，{files} 个文件，{users} 个用户，{locale} / {theme}。",
        "storage_loaded": "已加载原有仓库和设置。",
        "install_prompt": "输入 INSTALL 继续。",
        "need_storage": "安装必须指定存储目录：manage install --data 路径",
        "storage_invalid": "请使用绝对路径，且该目录不能包含本部署。",
        "storage_inside": "部署目录里只能使用 data/ 作为存储位置。",
        "storage_symlink": "存储路径不能是符号链接。",
        "storage_not_dir": "存储路径必须是目录。",
        "storage_parent": "存储目录的上一级必须已经存在。",
        "storage_unknown": "这个目录不是 Wercture 存储目录。未识别的内容：{names}",
        "storage_unreadable": "无法读取已有数据库。",
        "storage_new": "尚无数据库",
        "label_storage": "存储",
        "label_boot": "开机",
        "label_container": "容器",
        "container_running": "运行中",
        "container_stopped": "已停止",
        "container_unknown": "未知",
        "step_data": "准备宿主机数据目录。",
        "step_down": "停止并删除本部署的容器。",
        "step_image": "删除 wercture-server 镜像。",
        "step_cache": "清理 Docker 构建缓存。",
        "step_load": "载入发布包里的镜像。",
        "step_build": "不使用缓存重新构建镜像。",
        "step_up": "启动容器。",
        "step_unit": "写入开机项，不改变它是否已启用。",
        "desc_install": "选择存储目录；若已有仓库则直接加载，然后重新构建并启动。安装结束后清理构建缓存。",
        "desc_start": "启动当前容器。不改变开机自启。",
        "desc_restart": "重启当前容器。不改变开机自启。",
        "desc_status": "查看容器、开机项、语言和数据目录。",
        "desc_enable": "只开启开机自启。不启动也不停止容器。",
        "desc_disable": "只关闭开机自启。不启动也不停止容器。",
        "desc_config": "重置管理员密码，或选择 EN、ZH-CN。",
        "update": "更新",
        "desc_update": "检查 GitHub 稳定版，校验后替换镜像和管理程序。数据保留。",
        "update_prompt": "将替换本部署的镜像和管理程序，数据保留。输入 UPDATE 继续。",
        "update_none": "没有更新的稳定版。",
        "update_cli": "将替换镜像和管理程序，数据保留。继续？[y/N]",
        "update_done": "已更新。请重新打开 manage。数据仍保留。",
        "desc_uninstall": "删除本部署的容器、开机项和 Docker 镜像。保留仓库和配置。",
        "desc_password": "设置新的管理员密码。不修改用户名。",
        "desc_language": "选择全站语言：EN 或 ZH-CN。",
    },
}

MENU = ["install", "start", "restart", "status", "enable", "disable", "config", "update", "uninstall"]
CONFIG_MENU = ["password", "language", "back"]
LANGUAGE_MENU = ["english", "chinese", "back"]
LANGUAGE_VALUES = {"english": "en", "chinese": "zh-CN"}


class Failed(Exception):
    def __init__(self, message, code=1):
        super().__init__(message)
        self.code = code


class Result:
    def __init__(self, code, output):
        self.returncode = code
        self.stdout = output


def env_map():
    path = ROOT / ".env"
    values = {}
    try:
        lines = path.read_text().splitlines()
    except OSError:
        return values
    for line in lines:
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key.strip()] = value.strip()
    return values


def write_env(values):
    ordered = [key for key in ("WERCTURE_IMAGE", "WERCTURE_DATA") if values.get(key)]
    ordered.extend(key for key in values if key not in ordered and values.get(key))
    path = ROOT / ".env"
    path.write_text("".join(f"{key}={values[key]}\n" for key in ordered))
    os.chmod(path, 0o644)


def _contains(parent, child):
    try:
        child.resolve().relative_to(parent.resolve())
    except (OSError, ValueError):
        return False
    return child.resolve() != parent.resolve()


def storage_path():
    saved = env_map().get("WERCTURE_DATA", "").strip()
    if saved:
        return pathlib.Path(saved)
    override = os.environ.get("WERCTURE_DATA", "").strip()
    if override:
        return pathlib.Path(override)
    return ROOT / "data"


def suggested_storage():
    saved = env_map().get("WERCTURE_DATA", "").strip()
    if saved:
        return saved
    if (ROOT / "data" / "wercture.db").is_file():
        return str((ROOT / "data").resolve())
    return "/var/lib/wercture"


def resolve_storage(value):
    raw = str(value).strip()
    if (
        not raw
        or any(ord(char) < 32 for char in raw)
        or any(char in raw for char in " $`\\\"':")
    ):
        raise Failed(say("storage_invalid"))
    path = pathlib.Path(raw)
    if not path.is_absolute() or any(part in (".", "..") for part in path.parts):
        raise Failed(say("storage_invalid"))
    try:
        linked = path.is_symlink()
    except OSError as exc:
        raise Failed(say("storage_invalid")) from exc
    if linked:
        raise Failed(say("storage_symlink"))
    resolved = path.resolve(strict=False)
    if resolved == pathlib.Path("/") or resolved in BLOCKED_STORAGE:
        raise Failed(say("storage_invalid"))
    if any(_contains(blocked, resolved) for blocked in BLOCKED_STORAGE):
        raise Failed(say("storage_invalid"))
    root = ROOT.resolve()
    if resolved == root or _contains(resolved, root):
        raise Failed(say("storage_invalid"))
    if _contains(root, resolved) and resolved != (root / "data"):
        raise Failed(say("storage_inside"))
    if path.exists() and not path.is_dir():
        raise Failed(say("storage_not_dir"))
    if not path.exists() and not path.parent.is_dir():
        raise Failed(say("storage_parent"))
    return resolved


def inspect_storage(value):
    path = resolve_storage(value)
    files = path / "files"
    if files.exists() and (files.is_symlink() or not files.is_dir()):
        raise Failed(say("storage_not_dir"))
    if path.exists():
        unknown = sorted(item.name for item in path.iterdir() if item.name not in ALLOWED_NAMES)
        if unknown:
            raise Failed(say("storage_unknown").format(names=", ".join(unknown[:6])))
    info = {
        "path": path,
        "present": False,
        "warehouses": [],
        "images": 0,
        "files": 0,
        "users": 0,
        "locale": "",
        "theme": "",
    }
    if files.is_dir():
        info["files"] = sum(1 for item in files.iterdir() if item.is_file() and not item.is_symlink())
    database = path / "wercture.db"
    if not database.is_file() or database.is_symlink():
        return info
    try:
        connection = sqlite3.connect(f"file:{database}?mode=ro", uri=True, timeout=5)
        try:
            info["warehouses"] = [
                row[0] for row in connection.execute("SELECT name FROM warehouses ORDER BY id")
            ]
            info["images"] = connection.execute("SELECT COUNT(*) FROM images").fetchone()[0]
            info["users"] = connection.execute("SELECT COUNT(*) FROM users").fetchone()[0]
            row = connection.execute("SELECT locale, theme FROM settings WHERE id=1").fetchone()
        finally:
            connection.close()
    except sqlite3.Error as exc:
        raise Failed(f"{say('storage_unreadable')} ({exc})") from exc
    if row:
        info["locale"], info["theme"] = row
    info["present"] = True
    return info


def storage_text(info):
    path = info["path"]
    if info["present"]:
        names = ", ".join(info["warehouses"][:8]) or "-"
        if len(info["warehouses"]) > 8:
            names += " ..."
        return say("storage_found").format(
            path=path,
            warehouses=len(info["warehouses"]),
            names=names,
            images=info["images"],
            files=info["files"],
            users=info["users"],
            locale=info["locale"] or "-",
            theme=info["theme"] or "-",
        )
    if info["files"]:
        return say("storage_files_only").format(path=path, files=info["files"])
    return say("storage_empty").format(path=path)


def locale():
    database = storage_path() / "wercture.db"
    try:
        if not database.is_file() or database.is_symlink():
            return "en"
        connection = sqlite3.connect(f"file:{database}?mode=ro", uri=True, timeout=5)
        try:
            row = connection.execute("SELECT locale FROM settings WHERE id=1").fetchone()
        finally:
            connection.close()
    except (OSError, sqlite3.Error):
        return "en"
    if row and row[0] in TEXT:
        return row[0]
    return "en"


def say(key):
    return TEXT[locale()][key]


def require_root():
    if os.geteuid() != 0:
        raise Failed(say("need_root"))


def image_ref():
    override = os.environ.get("WERCTURE_IMAGE")
    if override:
        return override
    saved = env_map().get("WERCTURE_IMAGE", "")
    return saved or DEFAULT_IMAGE


def check_image_ref(name):
    if not re.fullmatch(r"wercture-server:[0-9]+\.[0-9]+\.[0-9]+", name):
        raise Failed("invalid image reference")


def write_image_ref(name):
    check_image_ref(name)
    values = env_map()
    values["WERCTURE_IMAGE"] = name
    write_env(values)


def current_version():
    build = ROOT / "BUILD.json"
    if build.is_file():
        try:
            value = json.loads(build.read_text()).get("version", "")
        except (OSError, json.JSONDecodeError, AttributeError):
            value = ""
        if isinstance(value, str) and value:
            return value
    try:
        value = (ROOT / "VERSION").read_text().strip()
    except OSError:
        value = ""
    return value or "0.0.0"


def run(args, check=True, input_text=None, quiet=False, image=None):
    env = os.environ.copy()
    env["WERCTURE_IMAGE"] = image or image_ref()
    env["WERCTURE_DATA"] = str(storage_path())
    proc = subprocess.Popen(
        args,
        cwd=ROOT,
        env=env,
        text=True,
        stdin=subprocess.PIPE if input_text is not None else subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )
    if input_text is None:
        chunks = []
        assert proc.stdout is not None
        for line in proc.stdout:
            text = line.replace("\r\n", "\n").replace("\r", "\n")
            if not quiet:
                print(text, end="")
                sys.stdout.flush()
            chunks.append(text)
        code = proc.wait()
        output = "".join(chunks)
    else:
        output, _ = proc.communicate(input_text)
        code = proc.returncode
        output = (output or "").replace("\r\n", "\n").replace("\r", "\n")
        if output and not quiet:
            print(output, end="" if output.endswith("\n") else "\n")
            sys.stdout.flush()
    if check and code != 0:
        if quiet:
            detail = (output or "").strip() or f"exit {code}"
        else:
            detail = f"command failed ({code})"
        raise Failed(detail, code)
    return Result(code, output or "")


def docker(*args, check=True, input_text=None, quiet=False, image=None):
    binary = shutil.which("docker")
    if not binary:
        raise Failed("docker is required")
    return run([binary, *args], check=check, input_text=input_text, quiet=quiet, image=image)


def compose(*args, check=True, input_text=None, quiet=False, project=None, image=None):
    command = ["compose", "-f", str(ROOT / "compose.yaml")]
    if project:
        command.extend(["-p", project])
    command.extend(args)
    return docker(*command, check=check, input_text=input_text, quiet=quiet, image=image)


def server_cmd(*args, password=None):
    running = compose("ps", "--status", "running", "-q", check=False, quiet=True)
    if running.stdout.strip():
        # exec does not apply the image entrypoint, so name the binary.
        return compose("exec", "-T", "wercture", "/wercture-server", *args, input_text=password)
    return compose(
        "run", "--rm", "--no-deps", "--entrypoint", "/wercture-server", "-T", "wercture", *args,
        input_text=password,
    )


def prepare_storage(path):
    path.mkdir(mode=0o700, exist_ok=True)
    (path / "files").mkdir(mode=0o700, exist_ok=True)
    if os.geteuid() == 0:
        for item in [path, *path.rglob("*")]:
            if item.is_symlink():
                continue
            os.chown(item, DATA_UID, DATA_UID, follow_symlinks=False)
            os.chmod(item, 0o700 if item.is_dir() else 0o600)
    print(f"storage: {path}")


def bundled_image():
    manifest = ROOT / "BUILD.json"
    if not manifest.is_file() or manifest.is_symlink():
        return None
    try:
        meta = json.loads(manifest.read_text())
    except (OSError, json.JSONDecodeError) as exc:
        raise Failed(f"invalid BUILD.json ({exc})") from exc
    image = str(meta.get("image", ""))
    archive_name = str(meta.get("image_archive", ""))
    check_image_ref(image)
    archive = ROOT / archive_name
    if (
        not archive_name
        or "/" in archive_name
        or archive_name in (".", "..")
        or archive.is_symlink()
        or not archive.is_file()
    ):
        raise Failed("bundled image archive is missing")
    return image, archive


def clear_build_cache():
    docker("builder", "prune", "-af")


def overview():
    path = storage_path()
    item = {
        "version": current_version(),
        "locale": locale(),
        "boot": boot_state(),
        "storage": str(path),
        "container": "unknown",
        "warehouses": 0,
        "images": 0,
        "present": False,
        "detail": say("storage_new"),
    }
    try:
        info = inspect_storage(path)
    except Failed as exc:
        item["detail"] = str(exc)
    else:
        item["present"] = info["present"]
        item["warehouses"] = len(info["warehouses"])
        item["images"] = info["images"]
        if info["locale"] in TEXT:
            item["locale"] = info["locale"]
        item["detail"] = storage_text(info).split("\n", 1)[-1]
    try:
        result = compose("ps", "--status", "running", "-q", check=False, quiet=True)
        item["container"] = "running" if result.stdout.strip() else "stopped"
    except Failed:
        item["container"] = "unknown"
    return item


def render_unit():
    text = (ROOT / "wercture.service").read_text()
    old = "WorkingDirectory=/opt/wercture"
    if old not in text.splitlines():
        raise Failed("unit template missing WorkingDirectory")
    return text.replace(old, f"WorkingDirectory={ROOT}", 1)


def unit_is_ours(path):
    if path.is_symlink() or not path.is_file():
        return False
    try:
        lines = path.read_text().splitlines()
    except OSError:
        return False
    return f"WorkingDirectory={ROOT}" in lines


def install_unit():
    require_root()
    if UNIT_PATH.is_symlink():
        raise Failed(say("unit_kept"))
    systemd = shutil.which("systemctl")
    if not systemd:
        raise Failed("systemctl is required")
    UNIT_PATH.write_text(render_unit())
    os.chmod(UNIT_PATH, 0o644)
    run([systemd, "daemon-reload"])
    print(f"unit: {UNIT_PATH}")


def remove_legacy_containers():
    """Remove only legacy Wercture containers proven to belong to this directory."""
    result = docker(
        "ps", "-aq", "--filter", "label=com.docker.compose.project=" + LEGACY_PROJECT,
        "--filter", "label=com.docker.compose.service=wercture", quiet=True,
    )
    for container in result.stdout.splitlines():
        inspected = docker("inspect", container, quiet=True)
        for item in json.loads(inspected.stdout):
            labels = item.get("Config", {}).get("Labels") or {}
            origin = labels.get("com.docker.compose.project.working_dir", "")
            if not origin or pathlib.Path(origin).resolve() != ROOT.resolve():
                continue
            if labels.get("com.docker.compose.project") != LEGACY_PROJECT or labels.get("com.docker.compose.service") != "wercture":
                continue
            image = item.get("Config", {}).get("Image", "")
            if not image.startswith("wercture-server:"):
                continue
            docker("rm", "-f", container)


def remove_images():
    listed = docker("images", "wercture-server", "--format", "{{.ID}}", check=False, quiet=True)
    ids = []
    for line in listed.stdout.splitlines():
        item = line.strip()
        if item and item not in ids:
            ids.append(item)
    if not ids:
        print("no wercture-server image")
        return
    docker("image", "rm", *ids)


def wait_health():
    deadline = time.time() + 60
    last = "not ready"
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(HEALTH_URL, timeout=3) as resp:
                body = resp.read().decode("utf-8", "replace").strip()
                print(f"health: {resp.status} {body}")
                if resp.status == 200:
                    return
                last = f"status {resp.status}"
        except (urllib.error.URLError, TimeoutError, OSError) as exc:
            last = str(exc)
            print(f"health: {last}")
        time.sleep(2)
    raise Failed(f"health check failed: {last}")


def install(storage=None):
    require_root()
    if not storage:
        raise Failed(say("need_storage"))
    info = inspect_storage(storage)
    bundle = bundled_image()
    requested = os.environ.get("WERCTURE_IMAGE", "").strip()
    image = bundle[0] if bundle else (requested or env_map().get("WERCTURE_IMAGE") or DEFAULT_IMAGE)
    check_image_ref(image)
    values = env_map()
    values["WERCTURE_IMAGE"] = image
    values["WERCTURE_DATA"] = str(info["path"])
    write_env(values)
    print(storage_text(info))
    print(say("step_data"))
    prepare_storage(info["path"])
    print(say("step_down"))
    remove_legacy_containers()
    compose("down", "--remove-orphans")
    print(say("step_image"))
    remove_images()
    if bundle:
        print(say("step_load"))
        docker("load", "-i", str(bundle[1]))
    else:
        print(say("step_cache"))
        clear_build_cache()
        print(say("step_build"))
        compose("build", "--no-cache")
    print(say("step_up"))
    compose("up", "-d", "--no-build", "--remove-orphans")
    print(say("step_unit"))
    install_unit()
    wait_health()
    print(say("step_cache"))
    clear_build_cache()
    if info["present"]:
        print(say("storage_loaded"))
    print(say("done"))
    print(f"storage: {info['path']}")


def start():
    compose("up", "-d", "--no-build", "--remove-orphans")
    print(say("done"))


def restart():
    compose("restart")
    print(say("done"))


def boot_state():
    if not UNIT_PATH.exists():
        return "absent"
    systemd = shutil.which("systemctl")
    if not systemd:
        return "unknown"
    result = run([systemd, "is-enabled", UNIT], check=False, quiet=True)
    return result.stdout.strip() or "unknown"


def status():
    compose("ps")
    path = storage_path()
    print(f"boot: {boot_state()}")
    print(f"version: {current_version()}")
    print(f"image: {image_ref()}")
    print(f"locale: {locale()}")
    print(f"storage: {path}")
    try:
        info = inspect_storage(path)
    except Failed as exc:
        print(exc)
    else:
        if info["present"] or info["files"]:
            print(storage_text(info))
    secret = path / "setup.secret"
    try:
        token = secret.read_text().strip() if secret.is_file() else ""
    except OSError as exc:
        print(f"setup: unreadable ({exc})")
        return
    if token:
        print(f"setup: http://127.0.0.1:9416/init{token}")


def enable():
    require_root()
    systemd = shutil.which("systemctl")
    if not systemd:
        raise Failed("systemctl is required")
    run([systemd, "enable", UNIT])
    print(say("boot_on"))


def disable():
    require_root()
    systemd = shutil.which("systemctl")
    if not systemd:
        raise Failed("systemctl is required")
    run([systemd, "disable", UNIT])
    print(say("boot_off"))


def reset_password(password=None):
    if password is None:
        first = getpass.getpass(say("new_password") + ": ")
        second = getpass.getpass(say("confirm_password") + ": ")
        if first != second:
            raise Failed(say("mismatch"))
        password = first
    size = len(password.encode())
    if size < 6 or size > 1024:
        raise Failed(say("password_length"))
    server_cmd("reset-password", "--data", "/data", password=password + "\n")
    print(say("done"))


def set_language(value):
    if value not in ("en", "zh-CN"):
        raise Failed(say("bad_language"))
    server_cmd("set-locale", "--data", "/data", value)
    print(say("done"))
    print(f"locale: {value}")


def remove_unit_if_ours():
    if not UNIT_PATH.exists() and not UNIT_PATH.is_symlink():
        return
    if not unit_is_ours(UNIT_PATH):
        print(say("unit_kept"))
        return
    require_root()
    systemd = shutil.which("systemctl")
    if systemd:
        run([systemd, "disable", UNIT], check=False)
        run([systemd, "stop", UNIT], check=False)
    UNIT_PATH.unlink()
    if systemd:
        run([systemd, "daemon-reload"], check=False)


def uninstall():
    require_root()
    path = storage_path()
    remove_legacy_containers()
    compose("down", "--remove-orphans")
    print(say("step_image"))
    remove_images()
    remove_unit_if_ours()
    print(say("data_kept"))
    print(f"storage: {path}")


ACTIONS = {
    "start": start,
    "restart": restart,
    "status": status,
    "enable": enable,
    "disable": disable,
    "uninstall": uninstall,
}


def execute(action, password=None, confirmed=False, cancel=None, storage=None):
    if action == "install":
        install(storage)
        return
    if action == "reset-password":
        reset_password(password)
        return
    if action == "update":
        import update
        try:
            update.execute(confirmed=confirmed, cancel=cancel)
        except RuntimeError as exc:
            raise Failed(str(exc)) from exc
        return
    ACTIONS[action]()


def main(argv):
    try:
        return dispatch(argv)
    except Failed as exc:
        print(exc, file=sys.stderr)
        return exc.code


def dispatch(argv):
    if len(argv) == 0:
        if not sys.stdin.isatty():
            usage()
            return 2
        import tui
        tui.run()
        return 0
    command = argv[0]
    if command == "update":
        if argv[1:] not in ([], ["--yes"]):
            usage()
            return 2
        execute("update", confirmed=argv[1:] == ["--yes"])
        return 0
    if command == "config" and len(argv) >= 2:
        if argv[1] == "reset-password" and len(argv) == 2:
            reset_password()
            return 0
        if argv[1] == "language" and len(argv) == 3:
            set_language(argv[2])
            return 0
    if command == "install" and argv[1:2] == ["--data"] and len(argv) == 3:
        install(argv[2])
        return 0
    if command in ACTIONS and len(argv) == 1:
        ACTIONS[command]()
        return 0
    usage()
    return 2


def usage():
    print(
        "manage [install --data PATH|start|restart|status|enable|disable|update|uninstall|config reset-password|config language en|zh-CN]",
        file=sys.stderr,
    )


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
