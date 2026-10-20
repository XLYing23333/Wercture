package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"wercture/server/internal/auth"
	"wercture/server/internal/server"
	"wercture/server/internal/store"
)

var version = "0.1.0"

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = run(os.Args[2:])
	case "health":
		err = health(os.Args[2:])
	case "reset-password":
		err = resetPassword(os.Args[2:])
	case "set-locale":
		err = setLocale(os.Args[2:])
	case "version":
		err = printVersion()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `wercture-server run --data DIR --listen ADDR
wercture-server health --url URL
wercture-server reset-password --data DIR
wercture-server set-locale --data DIR en|zh-CN
wercture-server version
`)
}

func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	data := fs.String("data", "/data", "data directory")
	listen := fs.String("listen", ":9416", "HTTP listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := store.Open(*data)
	if err != nil {
		return err
	}
	defer st.Close()
	hasAdmin, err := st.HasAdmin()
	if err != nil {
		return err
	}
	token, err := server.EnsureSetup(*data, hasAdmin)
	if err != nil {
		return err
	}
	if token != "" {
		log.Printf("setup path: /init%s", token)
	}
	app := server.New(st, auth.NewArgon())
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go app.PurgeLoop(ctx)
	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       time.Minute,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", *listen)
		errCh <- httpServer.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdown)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func health(args []string) error {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	raw := fs.String("url", "http://127.0.0.1:9416/health", "health URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(*raw)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}

func resetPassword(args []string) error {
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	data := fs.String("data", "/data", "data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Fprint(os.Stderr, "New password: ")
	password, err := readSecret()
	if err != nil {
		return err
	}
	if err := auth.ValidatePassword([]byte(password)); err != nil {
		return err
	}
	st, err := store.Open(*data)
	if err != nil {
		return err
	}
	defer st.Close()
	phc, err := auth.NewArgon().Hash([]byte(password))
	if err != nil {
		return err
	}
	return st.ResetAdminPassword(phc)
}

func setLocale(args []string) error {
	fs := flag.NewFlagSet("set-locale", flag.ContinueOnError)
	data := fs.String("data", "/data", "data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("locale must be en or zh-CN")
	}
	st, err := store.Open(*data)
	if err != nil {
		return err
	}
	defer st.Close()
	return st.SetLocale(fs.Arg(0))
}

func printVersion() error {
	fmt.Println("wercture-server " + version)
	return nil
}

func readSecret() (string, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return readTerminal()
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func readTerminal() (string, error) {
	fd := int(os.Stdin.Fd())
	state, err := termiosGet(fd)
	if err != nil {
		reader := bufio.NewReader(os.Stdin)
		line, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", readErr
		}
		fmt.Fprintln(os.Stderr)
		return strings.TrimRight(line, "\r\n"), nil
	}
	hidden := state
	hidden.Lflag &^= syscall.ECHO
	if err := termiosSet(fd, hidden); err != nil {
		return "", err
	}
	defer func() { _ = termiosSet(fd, state) }()
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	fmt.Fprintln(os.Stderr)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
