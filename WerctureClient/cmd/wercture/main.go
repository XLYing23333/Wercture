package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"wercture/client/internal/config"
	"wercture/client/internal/filekind"
	"wercture/client/internal/image"
)

var version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		os.Exit(runTUI())
	}
	var err error
	switch os.Args[1] {
	case "version", "--version":
		fmt.Println("wercture " + version)
	case "import":
		err = runImport(os.Args[2:])
	case "config":
		err = runConfig(os.Args[2:])
	case "upload":
		err = runUpload(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `wercture version
wercture import URL
wercture config list|rm|use|edit ...
wercture upload [--profile name] [--link url|typora|obsidian|markdown|html] [--compress] [--webp] [--none] [--quality N] files...
wercture upload --pdf files...
wercture upload --md files...
`)
}

func runImport(args []string) error {
	if len(args) != 1 {
		return errors.New("import URL")
	}
	profile, err := config.ParseImport(args[0])
	if err != nil {
		return err
	}
	db, err := config.Open("")
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Import(profile); err != nil {
		return err
	}
	fmt.Printf("imported %s\thost=%s\n", profile.Name, profile.Host)
	return nil
}

func runConfig(args []string) error {
	if len(args) == 0 {
		return errors.New("config needs add, list, edit, rm, or use")
	}
	db, err := config.Open("")
	if err != nil {
		return err
	}
	defer db.Close()
	switch args[0] {
	case "list":
		items, err := db.List()
		if err != nil {
			return err
		}
		for _, item := range items {
			mark := " "
			if item.Selected {
				mark = "*"
			}
			fmt.Printf("%s %s\thost=%s\tmode=%s\tquality=%d\n", mark, item.Name, item.Host, item.Mode, item.Quality)
		}
		return nil
	case "add":
		return addProfile(db, args[1:])
	case "edit":
		return editProfile(db, args[1:])
	case "rm":
		if len(args) != 2 {
			return errors.New("config rm NAME")
		}
		return db.Remove(args[1])
	case "use":
		if len(args) != 2 {
			return errors.New("config use NAME")
		}
		return db.Use(args[1])
	default:
		return errors.New("unknown config command")
	}
}

func addProfile(db *config.DB, args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	host := fs.String("host", "", "server origin")
	token := fs.String("token", "", "upload token")
	compress := fs.Bool("compress", false, "compress still images")
	webp := fs.Bool("webp", false, "convert still images to WebP")
	quality := fs.Int("quality", 80, "quality 1-100")
	ordered, err := flagsFirst(args, map[string]bool{"compress": true, "webp": true})
	if err != nil {
		return err
	}
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("config add NAME --host URL --token TOKEN")
	}
	return db.Add(config.Profile{Name: fs.Arg(0), Host: strings.TrimRight(*host, "/"), Token: *token, Mode: config.ModeFromSwitches(*compress, *webp), Quality: *quality})
}

func editProfile(db *config.DB, args []string) error {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	host := fs.String("host", "", "server origin")
	token := fs.String("token", "", "upload token")
	compress := fs.Bool("compress", false, "compress still images")
	webp := fs.Bool("webp", false, "convert still images to WebP")
	none := fs.Bool("none", false, "upload original bytes")
	quality := fs.Int("quality", 0, "quality 1-100")
	ordered, err := flagsFirst(args, map[string]bool{"compress": true, "webp": true, "none": true})
	if err != nil {
		return err
	}
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("config edit NAME")
	}
	mode, setMode, err := flagsMode(*none, *compress, *webp)
	if err != nil {
		return err
	}
	return db.Edit(fs.Arg(0), config.Profile{Host: strings.TrimRight(*host, "/"), Token: *token, Mode: mode, Quality: *quality}, setMode)
}

func flagsMode(none, compress, webp bool) (string, bool, error) {
	if none && (compress || webp) {
		return "", false, errors.New("--none cannot be combined with --compress or --webp")
	}
	if none {
		return "none", true, nil
	}
	if compress || webp {
		return config.ModeFromSwitches(compress, webp), true, nil
	}
	return "", false, nil
}

func runUpload(args []string) error {
	fs := flag.NewFlagSet("upload", flag.ContinueOnError)
	profileName := fs.String("profile", "", "saved profile")
	host := fs.String("host", "", "server origin")
	token := fs.String("token", "", "upload token")
	compress := fs.Bool("compress", false, "compress still images")
	webp := fs.Bool("webp", false, "convert still images to WebP")
	none := fs.Bool("none", false, "upload original bytes")
	quality := fs.Int("quality", 0, "quality 1-100")
	pdf := fs.Bool("pdf", false, "upload PDF files")
	md := fs.Bool("md", false, "upload Markdown files")
	linkStyle := fs.String("link", "url", "output url, typora, obsidian, markdown, or html")
	ordered, err := flagsFirst(args, map[string]bool{"compress": true, "webp": true, "none": true, "pdf": true, "md": true})
	if err != nil {
		return err
	}
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if _, err := formatLink(*linkStyle, "", "", ""); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("upload needs at least one file")
	}
	db, err := config.Open("")
	if err != nil {
		return err
	}
	defer db.Close()
	var saved config.Profile
	if *host == "" || *token == "" {
		if *profileName != "" {
			saved, err = db.Get(*profileName)
		} else {
			saved, err = db.Selected()
		}
		if err != nil && (*host == "" || *token == "") {
			return err
		}
	}
	if *host != "" {
		saved.Host = strings.TrimRight(*host, "/")
	}
	if *token != "" {
		saved.Token = *token
	}
	if saved.Host == "" || saved.Token == "" {
		return errors.New("host and token are required")
	}
	if (*pdf && *md) || ((*pdf || *md) && (*none || *compress || *webp)) {
		return errors.New("--pdf and --md cannot be combined with image processing")
	}
	flagMode, _, err := flagsMode(*none, *compress, *webp)
	if err != nil {
		return err
	}
	mode := image.Mode(config.EffectiveMode(saved.Mode, flagMode))
	qualityValue := config.EffectiveQuality(saved.Quality, *quality)
	client := &http.Client{Timeout: 2 * time.Minute}
	for _, name := range fs.Args() {
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		processed, err := prepareUpload(name, data, *pdf, *md, mode, qualityValue)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		link, err := postFile(client, saved.Host+"/api/v1/upload", saved.Token, processed)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		line, err := formatLink(*linkStyle, link, processed.Name, processed.MIME)
		if err != nil {
			return err
		}
		fmt.Println(line)
	}
	return nil
}

func prepareUpload(name string, data []byte, pdf, md bool, mode image.Mode, quality int) (image.Result, error) {
	base := path.Base(name)
	switch filekind.Classify(base, data) {
	case filekind.PDF:
		if !pdf {
			return image.Result{}, errors.New("pdf files need upload --pdf")
		}
		return image.Result{Data: data, Name: base, MIME: "application/pdf"}, nil
	case filekind.Markdown:
		if !md {
			return image.Result{}, errors.New("markdown files need upload --md")
		}
		return image.Result{Data: data, Name: base, MIME: "text/markdown"}, nil
	case filekind.Image:
		if pdf || md {
			return image.Result{}, errors.New("image files cannot use --pdf or --md")
		}
		return image.Process(data, base, mode, quality)
	default:
		if pdf {
			return image.Result{}, errors.New("not a pdf")
		}
		if md {
			return image.Result{}, errors.New("not a markdown file")
		}
		return image.Result{}, errors.New("unsupported file")
	}
}

func postFile(client *http.Client, endpoint, token string, file image.Result) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", file.Name)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(file.Data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload status %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	url := jsonString(raw, "url")
	if file.MIME == "text/markdown" {
		if render := jsonString(raw, "render_url"); render != "" {
			url = render
		}
	}
	if url == "" {
		return "", errors.New("upload response has no url")
	}
	return url, nil
}

// flagsFirst lets a name or file sit before its flags.
func flagsFirst(args []string, boolFlags map[string]bool) ([]string, error) {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") || boolFlags[name] {
			continue
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("missing value for %s", arg)
		}
		i++
		flags = append(flags, args[i])
	}
	return append(flags, positional...), nil
}

// formatLink shapes one uploaded URL for the caller.
// url, typora, and obsidian all print the bare link. Typora and Obsidian
// wrap that link themselves; markdown and html are inserted as-is.
func formatLink(style, rawURL, name, mime string) (string, error) {
	switch style {
	case "url", "typora", "obsidian":
		return rawURL, nil
	case "markdown":
		label := markdownLabel(name)
		if mime == "application/pdf" || mime == "text/markdown" {
			return "[" + label + "](" + rawURL + ")", nil
		}
		return "![" + label + "](" + rawURL + ")", nil
	case "html":
		label := htmlText(name)
		if mime == "application/pdf" || mime == "text/markdown" {
			return `<a href="` + rawURL + `">` + label + `</a>`, nil
		}
		return `<img src="` + rawURL + `" alt="` + label + `">`, nil
	default:
		return "", fmt.Errorf("unknown --link %s", style)
	}
}

func markdownLabel(name string) string {
	return strings.NewReplacer("\r", "", "\n", "", "]", " ").Replace(name)
}

func htmlText(name string) string {
	return strings.NewReplacer("\r", "", "\n", "", "&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(name)
}

func jsonString(raw []byte, key string) string {
	text := string(raw)
	needle := `"` + key + `":"`
	start := strings.Index(text, needle)
	if start < 0 {
		return ""
	}
	rest := text[start+len(needle):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}
