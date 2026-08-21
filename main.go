package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"

	"sdk_version_control/internal/config"
	"sdk_version_control/internal/logger"
	"sdk_version_control/internal/shim"
	"sdk_version_control/internal/shimmanager"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 1. If invoked as a shim (argv[0] is a known SDK command name), run the
	//    shim: it looks up the real binary and execs it.
	if shim.IsShimMode() {
		shim.Run()
		return
	}

	// 2. CLI subcommands (e.g. "svc init") run without launching the GUI.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init", "setup":
			runInitCLI()
			return
		case "version", "-v", "--version":
			printVersion()
			return
		case "help", "-h", "--help":
			printHelp()
			return
		}
	}

	// 3. Default: launch the Wails GUI application.
	app := NewApp()

	err := wails.Run(&options.App{
		Title:         "SDK Version Control",
		Width:         1200,
		Height:        800,
		DisableResize: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		// L2: use fmt.Fprintf(os.Stderr, ...) instead of the builtin println,
		// matching the error-reporting style used elsewhere in this file and
		// giving a guaranteed, formatted stderr message.
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}

// runInitCLI performs one-time shim setup from the command line.
// It creates the shims directory, installs the shim binary, and adds the
// single .svc.rc source line to the user's shell config (or registry PATH on
// Windows). After running "svc init", the user only needs to open a new shell.
func runInitCLI() {
	cfg, err := config.NewConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize config: %v\n", err)
		os.Exit(1)
	}

	// Honour a custom install path saved in settings.json
	sm := config.NewSettingsManager(cfg.SvcDir())
	if s := sm.Get(); s.InstallPath != "" {
		cfg.SetSvcDir(s.InstallPath)
	}

	logger.Init(cfg.SvcDir())

	mgr := shimmanager.New(cfg)
	if err := mgr.EnsureSetup(); err != nil {
		fmt.Fprintf(os.Stderr, "Setup failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("SVC shims setup complete.")
	fmt.Printf("  SVC home:  %s\n", cfg.SvcDir())
	fmt.Printf("  Shims dir: %s\n", cfg.ShimsDir())
	fmt.Printf("  RC file:   %s\n", cfg.RcFilePath())
	fmt.Println()
	fmt.Println("Open a new terminal window, then install/switch SDKs from the app.")
}

func printVersion() {
	var info AppInfo
	if err := json.Unmarshal(aboutJSON, &info); err != nil || info.Version == "" {
		fmt.Fprintln(os.Stdout, "svc 0.1.0")
		return
	}
	fmt.Fprintf(os.Stdout, "svc %s\n", info.Version)
}

func printHelp() {
	fmt.Fprintln(os.Stdout, "SVC - SDK Version Control")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  svc            Launch the GUI application")
	fmt.Fprintln(os.Stdout, "  svc init       One-time shim setup (creates ~/.svc/shims, .svc.rc)")
	fmt.Fprintln(os.Stdout, "  svc version    Print version")
	fmt.Fprintln(os.Stdout, "  svc help       Show this help")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "After installing an SDK, its commands (node, go, java, ...) are")
	fmt.Fprintln(os.Stdout, "available in every new shell via shims in ~/.svc/shims.")
}
