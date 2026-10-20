"""Curses menu. Navigation and command output stay on the terminal screen."""

import contextlib
import curses
import io
import threading
import unicodedata

import console

DESCRIPTIONS = {
    "install": "desc_install",
    "start": "desc_start",
    "restart": "desc_restart",
    "status": "desc_status",
    "enable": "desc_enable",
    "disable": "desc_disable",
    "config": "desc_config",
    "password": "desc_password",
    "language": "desc_language",
    "uninstall": "desc_uninstall",
    "update": "desc_update",
    "english": "desc_language",
    "chinese": "desc_language",
}


def cells(char):
    return 2 if unicodedata.east_asian_width(char) in ("W", "F") else 1


def width_of(value):
    return sum(cells(char) for char in str(value))


def clip(value, width):
    out = []
    used = 0
    for char in str(value):
        if unicodedata.category(char).startswith("C"):
            continue
        size = cells(char)
        if used + size > width:
            break
        out.append(char)
        used += size
    return "".join(out)


def pad(value, width):
    text = clip(value, width)
    return text + " " * max(0, width - width_of(text))


def wrap(value, width):
    width = max(2, width)
    lines = []
    for source in str(value).splitlines() or [""]:
        clean = "".join(char for char in source if not unicodedata.category(char).startswith("C"))
        if not clean:
            lines.append("")
            continue
        while clean:
            line = clip(clean, width)
            lines.append(line)
            clean = clean[len(line):]
    return lines


def put(screen, row, col, value, attr=0):
    height, width = screen.getmaxyx()
    if 0 <= row < height and 0 <= col < width - 1:
        with contextlib.suppress(curses.error):
            screen.addstr(row, col, clip(value, width - col - 1), attr)


def color(number):
    with contextlib.suppress(curses.error):
        if curses.has_colors():
            return curses.color_pair(number)
    return 0


def read_key(screen, timeout=-1):
    """Accept typed keys and both normal and application cursor sequences."""
    screen.timeout(timeout)
    try:
        key = screen.get_wch()
    except curses.error:
        return -1
    if key not in (27, "\x1b"):
        return key
    screen.timeout(25)
    try:
        try:
            first = screen.get_wch()
        except curses.error:
            return 27
        if first in (-1, ""):
            return 27
        if first not in ("[", "O"):
            with contextlib.suppress(curses.error):
                curses.unget_wch(first)
            return 27
        sequence = first
        for _ in range(5):
            try:
                char = screen.get_wch()
            except curses.error:
                return 27
            if char == -1:
                return 27
            sequence += char
            if char.isalpha() or char == "~":
                break
        return {
            "[A": curses.KEY_UP, "OA": curses.KEY_UP,
            "[B": curses.KEY_DOWN, "OB": curses.KEY_DOWN,
            "[H": curses.KEY_HOME, "OH": curses.KEY_HOME, "[1~": curses.KEY_HOME,
            "[F": curses.KEY_END, "OF": curses.KEY_END, "[4~": curses.KEY_END,
            "[5~": curses.KEY_PPAGE, "[6~": curses.KEY_NPAGE,
        }.get(sequence, 27)
    finally:
        screen.timeout(timeout)


def rule(screen, row, title=""):
    _, width = screen.getmaxyx()
    line = "─" * max(0, width - 3)
    put(screen, row, 1, line, color(1))
    if title:
        put(screen, row, 2, " " + title + " ", curses.A_BOLD | color(1))


def dialog(screen, title, prompt="", secret=False, initial="", notice=""):
    value = initial
    with contextlib.suppress(curses.error):
        curses.curs_set(1)
    try:
        while True:
            screen.erase()
            height, width = screen.getmaxyx()
            if height < 8 or width < 30:
                put(screen, 0, 0, "Resize terminal; Esc: back")
            else:
                rule(screen, 0, title)
                room = max(1, height - 8)
                for row, line in enumerate(wrap(prompt, width - 4)[:room], 2):
                    put(screen, row, 2, line)
                if notice:
                    put(screen, height - 6, 2, notice, color(4))
                shown = "*" * len(value) if secret else value
                while shown and width_of(clip(shown, width - 7)) < width_of(shown):
                    shown = shown[1:]
                put(screen, height - 4, 2, "> " + shown, curses.A_BOLD)
                put(screen, height - 2, 2, "Enter: submit   Esc: back   Ctrl-U: clear", curses.A_DIM)
                rule(screen, height - 1)
                with contextlib.suppress(curses.error):
                    column = 4 + width_of(shown)
                    screen.move(height - 4, min(width - 2, column))
            screen.refresh()
            key = read_key(screen)
            if key in (27, "\x1b", "\x03"):
                return None
            if key in ("\n", "\r", curses.KEY_ENTER) and height >= 8 and width >= 30:
                return value if secret else value.strip()
            if key == curses.KEY_RESIZE:
                continue
            if key == "\x15":
                value = ""
            elif key in (curses.KEY_BACKSPACE, "\x7f", "\b", curses.KEY_DC):
                value = value[:-1]
            elif isinstance(key, str) and key.isprintable() and len(value.encode()) < 1024:
                value += key
    finally:
        with contextlib.suppress(curses.error):
            curses.curs_set(0)


def container_label(state):
    return console.say({
        "running": "container_running",
        "stopped": "container_stopped",
    }.get(state, "container_unknown"))


def choose(screen, title, items, notice="", state=None):
    index = 0
    while True:
        screen.erase()
        height, width = screen.getmaxyx()
        if height < 12 or width < 36:
            put(screen, 0, 0, "Resize terminal; Esc/q: back")
            menu_top, menu_limit, footer = 2, 1, height
        else:
            version = state["version"] if state else console.current_version()
            rule(screen, 0, f"{title}  {version}")
            if state:
                mark = color(2) if state["container"] == "running" else color(4)
                put(screen, 1, 2, container_label(state["container"]), curses.A_BOLD | mark)
                put(
                    screen,
                    1,
                    16,
                    f"{console.say('label_boot')} {state['boot']}    {state['locale']}",
                    curses.A_DIM,
                )
                put(screen, 2, 2, f"{console.say('label_storage')}  {state['storage']}")
                put(screen, 3, 2, state["detail"], curses.A_DIM)
            rule(screen, 4)
            menu_top = 5
            footer = height - 5
            menu_limit = max(1, footer - menu_top)
            first = max(0, min(index - menu_limit + 1, len(items) - menu_limit))
            span = max(4, width - 4)
            for row, item in enumerate(items[first:first + menu_limit]):
                actual = first + row
                selected = actual == index
                attr = color(5) | curses.A_BOLD if selected else 0
                marker = "▸ " if selected else "  "
                put(screen, menu_top + row, 1, pad(marker + console.say(item), span), attr)
            rule(screen, footer)
            description = notice or console.say(DESCRIPTIONS.get(items[index], "hint"))
            for row, line in enumerate(wrap(description, width - 4)[:3], footer + 1):
                put(screen, row, 2, line, color(1))
            put(screen, height - 1, 1, console.say("hint"), curses.A_DIM)
        screen.refresh()
        key = read_key(screen)
        if key in (ord("q"), "q", 27, "\x1b", "\x03"):
            return None
        if key in (curses.KEY_UP, "k"):
            notice = ""
            index = (index - 1) % len(items)
        elif key in (curses.KEY_DOWN, "j"):
            notice = ""
            index = (index + 1) % len(items)
        elif key == curses.KEY_HOME:
            notice = ""
            index = 0
        elif key == curses.KEY_END:
            notice = ""
            index = len(items) - 1
        elif key in (ord("r"), ord("R")) and state is not None:
            notice = ""
            state = console.overview()
            continue
        elif key in (10, 13, "\n", "\r", curses.KEY_ENTER) and height >= 12 and width >= 36:
            return items[index]


class OperationOutput(io.StringIO):
    limit = 256 * 1024

    def __init__(self):
        super().__init__()
        self.lock = threading.Lock()

    def write(self, value):
        with self.lock:
            super().write(value)
            if self.tell() > self.limit:
                tail = super().getvalue()[-self.limit:]
                self.seek(0)
                self.truncate(0)
                super().write("[Earlier output omitted]\n" + tail)
        return len(value)

    def snapshot(self):
        with self.lock:
            return self.getvalue()


def result(screen, title, body, running=False, offset=0, outcome=""):
    screen.erase()
    height, width = screen.getmaxyx()
    lines = wrap(body, max(2, width - 4))
    count = max(1, height - 5)
    offset = max(0, min(offset, max(0, len(lines) - count)))
    status = "…" if running else outcome
    attr = color(3 if outcome == "Failed" else 2 if outcome == "Completed" else 1)
    rule(screen, 0, f"{title}  {status}")
    for row, line in enumerate(lines[offset:offset + count], 2):
        put(screen, row, 2, line)
    put(screen, height - 2, 2, f"{offset + 1}-{min(offset + count, len(lines))} / {len(lines)}", curses.A_DIM)
    footer = "Up/Down: scroll" if running else "Enter / Esc / q: back   Up/Down: scroll"
    put(screen, height - 1, 1, footer)
    screen.refresh()
    return offset


def operation(screen, action, password=None, confirmed=False, storage=None):
    title_key = "password" if action == "reset-password" else action
    title = console.say(title_key)
    output = OperationOutput()
    output.write("…\n")
    outcome = ["Completed"]
    cancel = threading.Event()

    def worker():
        try:
            with contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
                if action == "set-language":
                    console.set_language(password)
                else:
                    console.execute(
                        action,
                        password=password if action == "reset-password" else None,
                        confirmed=confirmed,
                        cancel=cancel,
                        storage=storage,
                    )
        except Exception as exc:
            outcome[0] = "Failed"
            output.write("\n" + str(exc).strip() + "\n")

    task = threading.Thread(target=worker, name="wercture-manage")
    offset = 10**9
    follow = True
    task.start()
    screen.timeout(100)
    try:
        while True:
            running = task.is_alive()
            shown = 10**9 if follow else offset
            offset = result(screen, title, output.snapshot(), running, shown, "" if running else outcome[0])
            key = read_key(screen, timeout=100)
            if running and action == "update" and key in (27, "\x1b", ord("q"), "q"):
                cancel.set()
                output.write("Cancelling download; the current deployment is unchanged.\n")
            if not running and key in (10, 13, "\n", "\r", 27, "\x1b", ord("q"), "q", curses.KEY_ENTER):
                break
            if key in (curses.KEY_UP, curses.KEY_PPAGE):
                follow = False
                offset -= 1 if key == curses.KEY_UP else max(1, screen.getmaxyx()[0] - 5)
            elif key in (curses.KEY_DOWN, curses.KEY_NPAGE):
                follow = False
                offset += 1 if key == curses.KEY_DOWN else max(1, screen.getmaxyx()[0] - 5)
            elif key == curses.KEY_HOME:
                follow = False
                offset = 0
            elif key == curses.KEY_END:
                follow = True
    finally:
        task.join()
        screen.timeout(-1)


def language_menu(screen):
    notice = ""
    while True:
        choice = choose(screen, console.say("language"), console.LANGUAGE_MENU, notice)
        if choice in (None, "back"):
            return
        operation(screen, "set-language", console.LANGUAGE_VALUES[choice])
        notice = ""


def config_menu(screen):
    notice = ""
    while True:
        choice = choose(screen, console.say("config"), console.CONFIG_MENU, notice)
        notice = ""
        if choice in (None, "back"):
            return
        if choice == "language":
            language_menu(screen)
            continue
        first = dialog(screen, console.say("password"), console.say("new_password"), secret=True)
        if first is None:
            notice = console.say("cancelled")
            continue
        second = dialog(screen, console.say("password"), console.say("confirm_password"), secret=True)
        if second is None:
            notice = console.say("cancelled")
            continue
        if first != second:
            notice = console.say("mismatch")
            continue
        operation(screen, "reset-password", first)


def install_flow(screen):
    initial = console.suggested_storage()
    notice = ""
    while True:
        entered = dialog(
            screen,
            console.say("install"),
            console.say("storage_prompt"),
            initial=initial,
            notice=notice,
        )
        if entered is None:
            return console.say("cancelled")
        initial = entered
        try:
            info = console.inspect_storage(entered)
        except console.Failed as exc:
            notice = str(exc)
            continue
        answer = dialog(
            screen,
            console.say("install"),
            console.storage_text(info) + "\n" + console.say("install_prompt"),
        )
        if answer is None:
            return console.say("cancelled")
        if answer != "INSTALL":
            notice = console.say("cancelled")
            continue
        operation(screen, "install", storage=str(info["path"]))
        return ""


def menu(screen):
    with contextlib.suppress(curses.error):
        curses.curs_set(0)
    screen.keypad(True)
    notice = ""
    state = console.overview()
    while True:
        choice = choose(screen, console.say("title"), console.MENU, notice, state)
        notice = ""
        if choice is None:
            return
        if choice == "config":
            config_menu(screen)
        elif choice == "install":
            notice = install_flow(screen)
        elif choice == "update":
            try:
                import update
                release = update.check()
                current = console.current_version()
                if update.version(release["tag_name"]) <= update.version(current):
                    notice = console.say("update_none")
                    state = console.overview()
                    continue
            except Exception as exc:
                notice = str(exc)
                state = console.overview()
                continue
            answer = dialog(screen, console.say("update"), console.say("update_prompt"))
            if answer != "UPDATE":
                notice = console.say("cancelled")
            else:
                operation(screen, "update", confirmed=True)
        elif choice == "uninstall":
            answer = dialog(
                screen,
                console.say("uninstall"),
                console.say("uninstall_prompt").format(path=console.storage_path()),
            )
            if answer != "UNINSTALL":
                notice = console.say("cancelled")
            else:
                operation(screen, choice)
        else:
            operation(screen, choice)
        state = console.overview()


def initialize(screen):
    if curses.has_colors():
        curses.start_color()
        with contextlib.suppress(curses.error):
            curses.use_default_colors()
            pairs = (
                (1, curses.COLOR_CYAN, -1),
                (2, curses.COLOR_GREEN, -1),
                (3, curses.COLOR_RED, -1),
                (4, curses.COLOR_YELLOW, -1),
                (5, curses.COLOR_BLACK, curses.COLOR_CYAN),
            )
            for number, foreground, background in pairs:
                with contextlib.suppress(curses.error):
                    curses.init_pair(number, foreground, background)
    menu(screen)


def run():
    with contextlib.suppress(AttributeError, curses.error):
        curses.set_escdelay(25)
    try:
        curses.wrapper(initialize)
    except curses.error as exc:
        raise console.Failed(f"terminal unavailable: {exc}") from None
