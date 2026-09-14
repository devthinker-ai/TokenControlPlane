package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/api"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/auth/upstream"
	"github.com/devthinker-ai/TokenControlPlane/pkg/billing"
	"github.com/devthinker-ai/TokenControlPlane/pkg/catalog"
	"github.com/devthinker-ai/TokenControlPlane/pkg/license"
	"github.com/devthinker-ai/TokenControlPlane/pkg/policy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/proxy"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
	"github.com/devthinker-ai/TokenControlPlane/pkg/update"
	stdiox "github.com/devthinker-ai/TokenControlPlane/pkg/upstream/stdio"
	"github.com/devthinker-ai/TokenControlPlane/pkg/web"
)

// Injected via -ldflags at build time (never hardcode release versions).
var (
	version = "dev"
	commit  = "none"
	built   = "unknown"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-version":
			path := envOr("DB_PATH", defaultDBPath())
			st, err := store.OpenWithOptions(path, store.OpenOptions{AppVersion: version})
			if err != nil {
				printVersion(nil)
				return
			}
			defer st.Close()
			printVersion(st)
			return
		case "update":
			os.Exit(runUpdate(os.Args[2:]))
		case "rollback":
			os.Exit(runRollback(os.Args[2:]))
		}
	}

	flag.Usage = printHelp
	showVersion := flag.Bool("version", false, "print version and exit")
	addr := flag.String("addr", envOr("ADDR", ":8080"), "listen address")
	dbPath := flag.String("db", envOr("DB_PATH", defaultDBPath()), "SQLite database path")
	flag.Parse()
	if *showVersion {
		st, err := store.OpenWithOptions(*dbPath, store.OpenOptions{AppVersion: version})
		if err != nil {
			printVersion(nil)
			return
		}
		defer st.Close()
		printVersion(st)
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	caps := license.Resolve(logger, os.Getenv("TOKENCONTROLPLANE_LICENSE_KEY"))

	st, err := store.OpenWithOptions(*dbPath, store.OpenOptions{AppVersion: version, Logger: logger})
	if err != nil {
		logger.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	validator, err := auth.NewValidator(st, auth.Config{Logger: logger})
	if err != nil {
		logger.Error("init auth", "err", err)
		os.Exit(1)
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "dev-insecure-jwt-secret-change-me"
		logger.Warn("JWT_SECRET unset — using insecure dev default")
	}
	sessions, err := session.NewManager(jwtSecret)
	if err != nil {
		logger.Error("init sessions", "err", err)
		os.Exit(1)
	}

	meter := proxy.NewMeter(st, validator)
	indexer := catalog.NewIndexer(st, logger)
	breaker := proxy.NewBreaker()
	bill := billing.New(st, billing.Config{Logger: logger})

	deviceProvider := &upstream.DeviceProvider{}
	pkceProvider := &upstream.PKCEProvider{
		RedirectURI: strings.TrimRight(func() string {
			u := os.Getenv("GATEWAY_URL")
			if u == "" {
				u = "http://localhost" + *addr
			}
			return u
		}(), "/") + "/oauth/redirect",
	}
	tokenGuard := upstream.NewTokenGuard(st, deviceProvider, logger)
	indexer.SetTokenGuard(tokenGuard)

	policyCache := policy.NewCache()
	if err := policyCache.Warm(context.Background(), st); err != nil {
		logger.Warn("policy cache warm failed", "err", err)
	}

	stdioMgr := stdiox.NewManager(st, breaker, logger, stdiox.Config{})
	stdioMgr.SetActivityEmitter(st)
	indexer.SetStdioIndexer(stdioMgr)

	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		logger.Warn("ADMIN_TOKEN unset — /admin/* will reject all requests")
	}

	gatewayURL := os.Getenv("GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://localhost" + *addr
	}

	disableRegister := os.Getenv("DISABLE_REGISTER") == "1"
	if disableRegister {
		logger.Info("public registration disabled (DISABLE_REGISTER=1); invite join still allowed")
	}

	apiDeps := api.Deps{
		Store:           st,
		Sessions:        sessions,
		Keys:            validator,
		Indexer:         indexer,
		Breaker:         breaker,
		Billing:         bill,
		GatewayURL:      gatewayURL,
		Caps:            caps,
		Version:         version,
		Commit:          commit,
		Built:           built,
		TokenGuard:      tokenGuard,
		DeviceProvider:  deviceProvider,
		PKCEProvider:    pkceProvider,
		Policy:          policyCache,
		Stdio:           stdioMgr,
		DisableRegister: disableRegister,
	}
	apiHandler := api.NewRouter(apiDeps)

	handler := proxy.NewRouter(proxy.Deps{
		Store:         st,
		Auth:          validator,
		Meter:         meter,
		Indexer:       indexer,
		Breaker:       breaker,
		AdminToken:    adminToken,
		Logger:        logger,
		API:           apiHandler,
		Webhook:       bill.Webhook,
		SPA:           web.Handler(),
		Version:       version,
		Commit:        commit,
		Built:         built,
		TokenGuard:    tokenGuard,
		OAuthRedirect: api.OAuthRedirect(apiDeps),
		Policy:        policyCache,
		Stdio:         stdioMgr,
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		schema, _ := st.SchemaVersion()
		logger.Info("gateway listening", "addr", *addr, "db", *dbPath, "version", version, "schema", schema, "plan", caps.Plan, "licensed", caps.Licensed)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = meter.Flush(ctx)
	_ = srv.Shutdown(ctx)
	stdioMgr.CloseAll()
	_ = st.Close()
	logger.Info("gateway stopped")
}

func printVersion(st *store.Store) {
	schema := 0
	if st != nil {
		schema, _ = st.SchemaVersion()
	}
	fmt.Printf("tokencontrolplane %s (commit %s, built %s, schema %d)\n", version, commit, built, schema)
}

func runUpdate(args []string) int {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	check := fs.Bool("check", false, "print current vs latest; exit 1 if newer available")
	file := fs.String("file", "", "install from local binary path (air-gap)")
	sha := fs.String("sha256", "", "expected sha256 for --file (or use local SHA256SUMS)")
	_ = fs.Parse(args)

	cfg := update.Config{
		CurrentVersion: version,
		UpdateURL:      os.Getenv("UPDATE_URL"),
	}

	if *check {
		info, err := update.Check(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "update check failed: %v\n", err)
			// Fail soft for operators — print current and exit 0? Prompt: exit 0 same, 1 newer.
			// Network error: treat as no update for --check? Prompt says silent available:false for API.
			fmt.Printf("current=%s latest=unknown available=false\n", version)
			return 0
		}
		fmt.Printf("current=%s latest=%s available=%v\n", info.Current, info.Latest, info.Available)
		if info.Available {
			return 1
		}
		return 0
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if *file != "" {
		expect := *sha
		if expect == "" {
			if b, err := os.ReadFile("SHA256SUMS"); err == nil {
				for _, line := range strings.Split(string(b), "\n") {
					fields := strings.Fields(line)
					if len(fields) >= 2 && fields[len(fields)-1] == filepath.Base(*file) {
						expect = fields[0]
					}
				}
			}
		}
		if expect == "" {
			fmt.Fprintln(os.Stderr, "update --file requires --sha256 or a matching entry in ./SHA256SUMS")
			return 1
		}
		if err := update.ApplyFile(exe, *file, expect); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	} else {
		if err := update.DownloadAndApply(cfg, exe); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	_, msg := update.MaybeRestartSystemd()
	fmt.Println(msg)
	return 0
}

func runRollback(args []string) int {
	fs := flag.NewFlagSet("rollback", flag.ExitOnError)
	dbPath := fs.String("db", envOr("DB_PATH", defaultDBPath()), "SQLite path (to detect schema advance)")
	_ = fs.Parse(args)

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	exe, _ = filepath.EvalSymlinks(exe)

	advanced := false
	var applied []int
	if st, err := store.OpenWithOptions(*dbPath, store.OpenOptions{AppVersion: version}); err == nil {
		applied, _ = st.ListAppliedMigrations()
		schema, _ := st.SchemaVersion()
		_ = st.Close()
		if schema > store.MaxEmbeddedMigrationID() {
			advanced = true
		}
	}

	if err := update.Rollback(exe, advanced, applied); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_, msg := update.MaybeRestartSystemd()
	fmt.Println(msg)
	return 0
}

func defaultDBPath() string {
	if v := os.Getenv("DB_PATH"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "tokencontrolplane.db"
	}
	return filepath.Join(home, ".tokencontrolplane", "gateway.db")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func printHelp() {
	fmt.Fprintf(os.Stderr, "TokenControlPlane — team MCP proxy with kill switch\n\n")
	fmt.Fprintf(os.Stderr, "Usage:\n  tokencontrolplane [flags]           run the gateway\n  tokencontrolplane version           print version\n  tokencontrolplane update [--check] [--file PATH] [--sha256 HEX]\n  tokencontrolplane rollback\n\nFlags:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nEnvironment:\n")
	w := tabwriter.NewWriter(os.Stderr, 0, 8, 2, ' ', 0)
	rows := [][2]string{
		{"ADDR", "Listen address (default :8080); overridable with -addr"},
		{"DB_PATH", "SQLite path (default ~/.tokencontrolplane/gateway.db); overridable with -db"},
		{"JWT_SECRET", "HS256 secret for dashboard JWT sessions (required in prod)"},
		{"ADMIN_TOKEN", "Bearer token for /admin/* design-partner API"},
		{"GATEWAY_URL", "Public base URL used in client snippets"},
		{"DISABLE_REGISTER", "Set to 1 to block POST /register (invite join still works)"},
		{"TOKENCONTROLPLANE_LICENSE_KEY", "RS256 license JWT (fail-open to free if unset/invalid)"},
		{"TOKENCONTROLPLANE_LICENSE_PRIVATE", "Path to RSA private key PEM (dashboard mint / reissue)"},
		{"LOOP_THRESHOLD", "Auto-kill threshold: requests per 60s (default 120)"},
		{"UPDATE_URL", "Override GitHub releases API for update checks/mirrors"},
		{"NO_UPDATE_CHECK", "Set to 1 to disable outbound update checks (air-gap)"},
		{"GITHUB_REPO", "owner/name for releases (default devthinker-ai/TokenControlPlane)"},
		{"LQ_SECRET_KEY", "Lemon Squeezy API secret (Bearer)"},
		{"LQ_WEBHOOK_SECRET", "Lemon Squeezy webhook signing secret"},
		{"LQ_STORE_ID", "Lemon Squeezy store ID"},
		{"LQ_VARIANT_ID_PRO", "LS variant ID for Pro one-time product"},
		{"LQ_VARIANT_ID_TEAM", "LS variant ID for Team one-time product"},
		{"LQ_RETURN_URL", "Post-checkout redirect (default GATEWAY_URL/billing?success=1)"},
	}
	for _, row := range rows {
		fmt.Fprintf(w, "  %s\t%s\n", row[0], row[1])
	}
	_ = w.Flush()
	fmt.Fprintf(os.Stderr, "\nSelf-hosted licensing is offline, one-time purchase via Lemon Squeezy (merchant of record — they handle VAT/sales tax/refunds). Keys are bound to plan caps, not machines.\n")
}
