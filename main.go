// Command discuss-watcher forwards le discussions to Discord webhooks
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"discuss-watcher/internal"
)

type webhookList []string

func (l *webhookList) String() string { return strings.Join(*l, ",") }

func (l *webhookList) Set(value string) error {
	for hook := range strings.SplitSeq(value, ",") {
		if hook = strings.TrimSpace(hook); hook != "" {
			*l = append(*l, hook)
		}
	}
	return nil
}

func main() {
	var hooks webhookList
	wiki := flag.String("wiki", "", "base URL of the wiki, e.g. https://tds.wiki")
	flag.Var(&hooks, "webhook", "Discord webhook URL; repeat or comma-separate for several")
	interval := flag.Duration("interval", 30*time.Second, "time between polls")
	limit := flag.Int("limit", 20, "posts to fetch per poll (1-100)")
	stateFile := flag.String("state", "discuss-watcher.json", "file storing the last seen post id")
	compact := flag.Bool("compact", false, "send compact messages instead of embeds")
	hideIPs := flag.Bool("hide-ips", false, `show "Unregistered user" instead of an IP address`)
	dryRun := flag.Bool("dry-run", false, "print payloads instead of sending them")
	once := flag.Bool("once", false, "poll once and exit")
	timeout := flag.Duration("timeout", 15*time.Second, "HTTP request timeout")
	types := flag.String("types", "", "container types to watch: forum, wall, comments (default all)")
	verbose := flag.Bool("v", false, "log debug information")
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	base := strings.TrimRight(strings.TrimSpace(*wiki), "/")
	if base == "" {
		slog.Error("-wiki is required")
		flag.Usage()
		os.Exit(2)
	}
	if u, err := url.Parse(base); err != nil || u.Scheme == "" || u.Host == "" {
		slog.Error("invalid -wiki URL", "wiki", *wiki)
		os.Exit(2)
	}
	if len(hooks) == 0 && !*dryRun {
		slog.Error("at least one -webhook is required (or use -dry-run)")
		os.Exit(2)
	}
	watched, err := watcher.ParseTypes(*types)
	if err != nil {
		slog.Error("invalid -types", "error", err)
		os.Exit(2)
	}

	w := watcher.New(watcher.Config{
		Wiki:     base,
		Webhooks: hooks,
		Interval: *interval,
		Limit:    min(max(*limit, 1), 100),
		State:    *stateFile,
		Compact:  *compact,
		HideIPs:  *hideIPs,
		DryRun:   *dryRun,
		Once:     *once,
		Types:    watched,
		Client:   &http.Client{Timeout: *timeout},
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("watcher stopped", "error", err)
		os.Exit(1)
	}
}
