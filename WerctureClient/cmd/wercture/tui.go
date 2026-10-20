package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"wercture/client/internal/config"
	"wercture/client/internal/image"
)

type menuText struct {
	title, current, none, importURL, switchProfile, compress, toWebP, upload, status, remove, quit string
	promptURL, promptFiles, imported, used, removed, uploaded, needProfile                         string
	host, token, quality, server, online, offline, on, off, keys, inputHint                        string
}

func texts(locale string) menuText {
	if locale == "zh-CN" {
		return menuText{
			title: "Wercture", current: "当前配置", none: "还没有配置",
			importURL: "导入 URL", switchProfile: "切换配置", compress: "压缩", toWebP: "转 WebP",
			upload: "上传文件", status: "Status", remove: "删除当前配置", quit: "退出",
			promptURL: "导入 URL", promptFiles: "文件路径，多个用空格分开",
			imported: "已导入", used: "已切换", removed: "已删除", uploaded: "已上传",
			needProfile: "先导入一个配置",
			host:        "地址", token: "密钥", quality: "质量", server: "服务器",
			online: "在线", offline: "离线", on: "开", off: "关",
			keys:      "方向键选择 | Enter 执行 | Esc 返回 | Q 退出",
			inputHint: "Enter 确认 | Esc 返回",
		}
	}
	return menuText{
		title: "Wercture", current: "Profile", none: "No profile",
		importURL: "Import URL", switchProfile: "Switch profile", compress: "Compress", toWebP: "WebP",
		upload: "Upload files", status: "Status", remove: "Delete current profile", quit: "Quit",
		promptURL: "Import URL", promptFiles: "File paths, separated by spaces",
		imported: "Imported", used: "Selected", removed: "Deleted", uploaded: "Uploaded",
		needProfile: "Import a profile first",
		host:        "Host", token: "Token", quality: "Quality", server: "Server",
		online: "online", offline: "offline", on: "on", off: "off",
		keys:      "Up/Down select | Enter execute | Esc back | q quit",
		inputHint: "Enter confirms | Esc back",
	}
}

func runTUI() int {
	term, err := openTerminal()
	if err != nil {
		usage()
		return 2
	}
	defer term.Close()
	db, err := config.Open("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer db.Close()
	copy := texts("en")
	localeHost := "\x00"
	screen := "menu"
	selected := 0
	message := ""
	var profiles []config.Profile
	for {
		profile, profileErr := db.Selected()
		has := profileErr == nil
		host := ""
		if has {
			host = profile.Host
		}
		if host != localeHost {
			localeHost = host
			copy = texts(siteLocale(host))
		}
		items := menuItems(copy, profile, has)
		if screen == "switch" {
			profiles, err = db.List()
			if err != nil {
				screen = "menu"
				message = err.Error()
			} else if len(profiles) == 0 {
				screen = "menu"
				message = copy.needProfile
			} else {
				items = make([]string, len(profiles))
				for i, item := range profiles {
					mark := "  "
					if item.Selected {
						mark = "* "
					}
					items[i] = mark + item.Name
				}
			}
		}
		draw(term, copy, items, selected, profile, has, screen == "menu", message)
		key, err := term.Key()
		if err != nil {
			return 0
		}
		switch key {
		case "up":
			selected = (selected + len(items) - 1) % len(items)
		case "down":
			selected = (selected + 1) % len(items)
		case "esc":
			if screen == "switch" {
				screen = "menu"
				selected = 1
				message = ""
			}
		case "q", "Q":
			return 0
		case "enter":
			if screen == "switch" {
				if err := db.Use(profiles[selected].Name); err != nil {
					message = err.Error()
				} else {
					message = copy.used + " " + profiles[selected].Name
				}
				screen = "menu"
				selected = 1
				continue
			}
			message = act(db, term, copy, selected, profile, has)
			if message == "quit" {
				return 0
			}
			if message == "switch" {
				screen = "switch"
				selected = 0
				message = ""
			}
		}
	}
}

func act(db *config.DB, term terminal, copy menuText, selected int, profile config.Profile, ok bool) string {
	switch selected {
	case 0:
		raw, okInput := readInput(term, copy, copy.promptURL)
		if !okInput {
			return ""
		}
		parsed, err := config.ParseImport(raw)
		if err != nil {
			return err.Error()
		}
		if err := db.Import(parsed); err != nil {
			return err.Error()
		}
		return copy.imported + " " + parsed.Name
	case 1:
		return "switch"
	case 2:
		return toggleSwitch(db, copy, profile, ok, "compress")
	case 3:
		return toggleSwitch(db, copy, profile, ok, "webp")
	case 4:
		if !ok {
			return copy.needProfile
		}
		raw, okInput := readInput(term, copy, copy.promptFiles)
		if !okInput {
			return ""
		}
		return uploadPaths(profile, strings.Fields(raw), copy)
	case 5:
		return statusText(copy, profile, ok)
	case 6:
		if !ok {
			return copy.needProfile
		}
		if err := db.Remove(profile.Name); err != nil {
			return err.Error()
		}
		return copy.removed
	default:
		return "quit"
	}
}

func readInput(term terminal, copy menuText, title string) (string, bool) {
	value := ""
	for {
		rows, cols := term.Size()
		var b strings.Builder
		b.WriteString("\x1b[H\x1b[2J\x1b[?25h")
		fmt.Fprintln(&b, clip(title, cols-1))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, clip(copy.inputHint, cols-1))
		fmt.Fprintln(&b)
		shown := "> " + value
		fmt.Fprintln(&b, clip(shown, max(cols-1, 1)))
		fmt.Fprintf(&b, "\x1b[%d;1H%s", rows, clip(copy.inputHint, cols-1))
		col := 1 + runeWidth(shown)
		if col > cols {
			col = cols
		}
		fmt.Fprintf(&b, "\x1b[5;%dH", col)
		term.Write(b.String())
		key, err := term.Key()
		if err != nil {
			return "", false
		}
		switch key {
		case "esc":
			return "", false
		case "enter":
			if strings.TrimSpace(value) == "" {
				return "", false
			}
			return strings.TrimSpace(value), true
		case "backspace":
			runes := []rune(value)
			if len(runes) > 0 {
				value = string(runes[:len(runes)-1])
			}
		case "up", "down":
		default:
			if strings.TrimSpace(key) != "" && len(value)+len(key) <= 4096 {
				value += key
			}
		}
	}
}

func uploadPaths(profile config.Profile, paths []string, copy menuText) string {
	client := &http.Client{Timeout: 2 * time.Minute}
	mode := image.Mode(config.EffectiveMode(profile.Mode, ""))
	quality := config.EffectiveQuality(profile.Quality, 0)
	var links []string
	for _, name := range paths {
		data, err := os.ReadFile(name)
		if err != nil {
			return err.Error()
		}
		processed, err := prepareUpload(name, data, false, false, mode, quality)
		if err != nil {
			return fmt.Sprintf("%s: %s", name, err.Error())
		}
		link, err := postFile(client, profile.Host+"/api/v1/upload", profile.Token, processed)
		if err != nil {
			return fmt.Sprintf("%s: %s", name, err.Error())
		}
		links = append(links, link)
	}
	if len(links) == 0 {
		return ""
	}
	return copy.uploaded + "\n" + strings.Join(links, "\n")
}

func menuItems(copy menuText, profile config.Profile, ok bool) []string {
	compressOn, webpOn := false, false
	if ok {
		compressOn, webpOn = config.Switches(profile.Mode)
	}
	return []string{
		copy.importURL,
		copy.switchProfile,
		switchItem(copy.compress, compressOn, "33", copy.on, copy.off),
		switchItem(copy.toWebP, webpOn, "36", copy.on, copy.off),
		copy.upload,
		copy.status,
		copy.remove,
		copy.quit,
	}
}

func toggleSwitch(db *config.DB, copy menuText, profile config.Profile, ok bool, which string) string {
	if !ok {
		return copy.needProfile
	}
	compressOn, webpOn := config.Switches(profile.Mode)
	label, color := copy.compress, "33"
	if which == "compress" {
		compressOn = !compressOn
	} else {
		webpOn = !webpOn
		label, color = copy.toWebP, "36"
	}
	mode := config.ModeFromSwitches(compressOn, webpOn)
	if err := db.Edit(profile.Name, config.Profile{Mode: mode, Quality: profile.Quality}, true); err != nil {
		return err.Error()
	}
	on := compressOn
	if which != "compress" {
		on = webpOn
	}
	return switchItem(label, on, color, copy.on, copy.off)
}

func switchItem(label string, on bool, onCode, onWord, offWord string) string {
	if on {
		return label + "  \x1b[" + onCode + "m" + onWord + "\x1b[0m"
	}
	return label + "  \x1b[2m" + offWord + "\x1b[0m"
}

func infoLine(label, value string) string {
	return "\x1b[90m" + label + ": \x1b[0m" + value
}

func statusText(copy menuText, profile config.Profile, ok bool) string {
	if !ok {
		return copy.needProfile
	}
	compressOn, webpOn := config.Switches(profile.Mode)
	server := serverState(profile.Host, copy)
	if server == copy.online {
		server = "\x1b[32m" + server + "\x1b[0m"
	} else {
		server = "\x1b[31m" + server + "\x1b[0m"
	}
	return strings.Join([]string{
		infoLine(copy.current, profile.Name),
		infoLine(copy.host, profile.Host),
		infoLine(copy.token, maskToken(profile.Token)),
		switchItem(copy.compress, compressOn, "33", copy.on, copy.off),
		switchItem(copy.toWebP, webpOn, "36", copy.on, copy.off),
		infoLine(copy.quality, fmt.Sprintf("%d", config.EffectiveQuality(profile.Quality, 0))),
		infoLine(copy.server, server),
	}, "\n")
}

func maskToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + strings.Repeat("*", len(token)-8) + token[len(token)-4:]
}

func siteLocale(host string) string {
	host = strings.TrimRight(host, "/")
	if host == "" {
		return "en"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get(host + "/api/v1/settings")
	if err != nil {
		return "en"
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil || res.StatusCode != http.StatusOK {
		return "en"
	}
	return localeFromSettings(string(body))
}

func localeFromSettings(body string) string {
	compact := strings.ReplaceAll(body, " ", "")
	if strings.Contains(compact, `"locale":"zh-CN"`) {
		return "zh-CN"
	}
	return "en"
}

func serverState(host string, copy menuText) string {
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get(strings.TrimRight(host, "/") + "/health")
	if err != nil {
		return copy.offline
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != http.StatusOK {
		return copy.offline
	}
	return copy.online
}

func draw(term terminal, copy menuText, items []string, selected int, profile config.Profile, has, root bool, message string) {
	rows, cols := term.Size()
	var b strings.Builder
	b.WriteString("\x1b[H\x1b[2J\x1b[?25l")
	fmt.Fprintln(&b, clip(copy.title, cols-1))
	if root && has {
		compressOn, webpOn := config.Switches(profile.Mode)
		fmt.Fprintln(&b, clip(copy.current+": "+profile.Name, cols-1))
		fmt.Fprintln(&b, clip(profile.Host, cols-1))
		fmt.Fprintln(&b, clip(switchItem(copy.compress, compressOn, "33", copy.on, copy.off)+"   "+switchItem(copy.toWebP, webpOn, "36", copy.on, copy.off), cols-1))
	} else if root {
		fmt.Fprintln(&b, clip(copy.none, cols-1))
	} else {
		fmt.Fprintln(&b, clip(copy.switchProfile, cols-1))
	}
	fmt.Fprintln(&b)
	visible := rows - 9
	if visible < 1 {
		visible = 1
	}
	first := 0
	if selected >= visible {
		first = selected - visible + 1
	}
	last := first + visible
	if last > len(items) {
		last = len(items)
	}
	for i := first; i < last; i++ {
		line := clip("  "+items[i], cols-1)
		if i == selected {
			line = "\x1b[7m> " + clip(stripANSI(items[i]), cols-3) + "\x1b[0m"
		}
		fmt.Fprintln(&b, line)
	}
	fmt.Fprintln(&b)
	used := 6 + (last - first)
	for _, line := range strings.Split(message, "\n") {
		if used >= rows-2 {
			break
		}
		shown := clip(line, cols-1)
		if !strings.Contains(line, "\x1b[") {
			shown = "\x1b[32m" + shown + "\x1b[0m"
		}
		fmt.Fprintln(&b, shown)
		used++
	}
	fmt.Fprintf(&b, "\x1b[%d;1H%s", rows, clip(copy.keys, cols-1))
	term.Write(b.String())
}

func clip(s string, columns int) string {
	if columns < 1 {
		return ""
	}
	var out strings.Builder
	used := 0
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == 0x1b && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) && ((runes[j] >= '0' && runes[j] <= '9') || runes[j] == ';') {
				j++
			}
			if j < len(runes) && runes[j] == 'm' {
				out.WriteString(string(runes[i : j+1]))
				i = j
				continue
			}
		}
		if r < 32 || r == 127 {
			continue
		}
		w := 1
		if r >= 0x1100 {
			w = 2
		}
		if used+w > columns {
			break
		}
		out.WriteRune(r)
		used += w
	}
	return out.String()
}

func stripANSI(s string) string {
	var out strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) && ((runes[j] >= '0' && runes[j] <= '9') || runes[j] == ';') {
				j++
			}
			if j < len(runes) && runes[j] == 'm' {
				i = j
				continue
			}
		}
		out.WriteRune(runes[i])
	}
	return out.String()
}

func runeWidth(s string) int {
	used := 0
	for _, r := range s {
		if r < 32 || r == 127 {
			continue
		}
		if r >= 0x1100 {
			used += 2
			continue
		}
		used++
	}
	return used
}
