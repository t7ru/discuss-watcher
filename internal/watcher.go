// Package watcher forwards ze discussions to Discord webhooks
package watcher

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

const userAgent = "discuss-watcher/1.0"

type Config struct {
	Wiki        string
	ArticlePath string
	Webhooks    []string
	Interval    time.Duration
	Limit       int
	State       string
	Compact     bool
	HideIPs     bool
	DryRun      bool
	Once        bool
	Types       map[string]bool
	Client      *http.Client
}

type Watcher struct {
	cfg    Config
	api    *apiClient
	cursor string
}

func New(cfg Config) *Watcher {
	return &Watcher{
		cfg:    cfg,
		api:    &apiClient{base: cfg.Wiki, client: cfg.Client},
		cursor: loadCursor(cfg.State, cfg.Wiki),
	}
}

func ParseTypes(list string) (map[string]bool, error) {
	if strings.TrimSpace(list) == "" {
		return nil, nil
	}
	types := make(map[string]bool)
	for name := range strings.SplitSeq(list, ",") {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "":
		case "forum":
			types["FORUM"] = true
		case "wall":
			types["WALL"] = true
		case "comment", "comments", "article_comment":
			types["ARTICLE_COMMENT"] = true
		default:
			return nil, fmt.Errorf("unknown type %q (want forum, wall or comments)", name)
		}
	}
	return types, nil
}

func (c *Config) watches(container string) bool {
	return len(c.Types) == 0 || c.Types[container]
}

func (w *Watcher) Run(ctx context.Context) error {
	slog.Info("watching discussions", "wiki", w.cfg.Wiki, "webhooks", len(w.cfg.Webhooks),
		"interval", w.cfg.Interval, "cursor", w.cursor)
	err := w.poll(ctx)
	if w.cfg.Once {
		return err
	}
	if err != nil && ctx.Err() == nil {
		slog.Error("poll failed", "error", err)
	}
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.poll(ctx); err != nil && ctx.Err() == nil {
				slog.Error("poll failed", "error", err)
			}
		}
	}
}

func (w *Watcher) poll(ctx context.Context) error {
	if w.cfg.ArticlePath == "" {
		if path, err := w.api.getArticlePath(ctx); err == nil {
			w.cfg.ArticlePath = path
			slog.Info("using the wiki's article path", "path", path)
		} else {
			slog.Debug("could not discover the article path", "error", err)
		}
	}
	posts, err := w.api.getPosts(ctx, w.cfg.Limit)
	if err != nil {
		return err
	}
	if len(posts) == 0 {
		return nil
	}
	cursor := w.cursor
	if cursor == "" {
		if !w.cfg.DryRun {
			newest := newestID(posts)
			slog.Info("first run; starting from the newest post", "cursor", newest)
			return w.save(newest)
		}
		cursor = "0" // for dry
	}
	var fresh []*post
	for _, p := range slices.Backward(posts) { // api returns newest first
		if p.ID > cursor && w.cfg.watches(p.containerType()) {
			fresh = append(fresh, p)
		}
	}
	if len(fresh) == 0 {
		return w.save(newestID(posts))
	}

	var pages map[string]articleName
	seen := make(map[string]bool)
	var ids []string
	for _, p := range fresh {
		if p.containerType() == "ARTICLE_COMMENT" && !seen[p.ForumID] {
			seen[p.ForumID] = true
			ids = append(ids, p.ForumID)
		}
	}
	if len(ids) > 0 {
		pages, err = w.api.getArticleNames(ctx, ids)
		if err != nil {
			slog.Warn("article name lookup failed", "error", err)
		}
	}

	slog.Info("new discussion posts", "count", len(fresh), "cursor", newestID(posts))
	payloads := make([]webhookPayload, 0, len(fresh))
	for _, p := range fresh {
		payloads = append(payloads, w.cfg.message(p, pages[p.ForumID]))
	}
	w.send(ctx, payloads)
	return w.save(newestID(posts))
}

func newestID(posts []*post) string {
	newest := posts[0].ID
	for _, p := range posts[1:] {
		if p.ID > newest {
			newest = p.ID
		}
	}
	return newest
}

type state struct {
	Wiki   string `json:"wiki"`
	Cursor string `json:"cursor"`
}

func (w *Watcher) save(cursor string) error {
	if w.cfg.DryRun || cursor <= w.cursor {
		return nil
	}
	w.cursor = cursor
	data, err := json.Marshal(state{Wiki: w.cfg.Wiki, Cursor: cursor})
	if err != nil {
		return err
	}
	return os.WriteFile(w.cfg.State, append(data, '\n'), 0o644)
}

func loadCursor(path, wiki string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var s state
	if json.Unmarshal(data, &s) != nil || s.Wiki != wiki {
		return ""
	}
	return s.Cursor
}

func (w *Watcher) send(ctx context.Context, payloads []webhookPayload) {
	batches := w.cfg.batches(payloads)
	if w.cfg.DryRun {
		for _, batch := range batches {
			data, err := json.Marshal(batch)
			if err != nil {
				slog.Error("encoding payload", "error", err)
				continue
			}
			fmt.Println(string(data))
		}
		return
	}
	for _, hook := range w.cfg.Webhooks {
		for _, batch := range batches {
			if err := postWebhook(ctx, w.cfg.Client, hook, batch); err != nil {
				slog.Error("discord send failed", "webhook", webhookID(hook), "error", err)
			}
		}
	}
}

func (c *Config) batches(payloads []webhookPayload) []webhookPayload {
	if len(payloads) == 0 {
		return nil
	}
	mentions := allowedMentions{Parse: []string{}}
	if c.Compact {
		var batches []webhookPayload
		var content strings.Builder
		length := 0
		for _, payload := range payloads {
			n := len([]rune(payload.Content))
			if length > 0 && length+1+n > 2000 {
				batches = append(batches, webhookPayload{AllowedMentions: mentions, Content: content.String()})
				content.Reset()
				length = 0
			}
			if length > 0 {
				content.WriteByte('\n')
				length++
			}
			content.WriteString(payload.Content)
			length += n
		}
		return append(batches, webhookPayload{AllowedMentions: mentions, Content: content.String()})
	}
	var batches []webhookPayload
	var embeds []embed
	total := 0
	for _, payload := range payloads {
		e := payload.Embeds[0]
		n := e.length()
		if len(embeds) > 0 && (len(embeds) >= 10 || total+n > 6000) {
			batches = append(batches, webhookPayload{AllowedMentions: mentions, Embeds: embeds})
			embeds, total = nil, 0
		}
		embeds = append(embeds, e)
		total += n
	}
	return append(batches, webhookPayload{AllowedMentions: mentions, Embeds: embeds})
}

func postWebhook(ctx context.Context, client *http.Client, hook string, payload webhookPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := range 5 {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", userAgent)
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if err := sleepCtx(ctx, backoff(attempt)); err != nil {
				return err
			}
			continue
		}
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			resp.Body.Close()
			return nil
		case resp.StatusCode == http.StatusTooManyRequests:
			var rate struct {
				RetryAfter float64 `json:"retry_after"`
			}
			_ = json.UnmarshalRead(io.LimitReader(resp.Body, 4096), &rate)
			resp.Body.Close()
			wait := min(time.Duration(rate.RetryAfter*float64(time.Second)), time.Minute)
			lastErr = errors.New("rate limited")
			if wait <= 0 {
				wait = time.Second
			}
			if err := sleepCtx(ctx, wait); err != nil {
				return err
			}
		case resp.StatusCode >= 500:
			resp.Body.Close()
			lastErr = fmt.Errorf("discord returned %s", resp.Status)
			if err := sleepCtx(ctx, backoff(attempt)); err != nil {
				return err
			}
		default:
			msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			return fmt.Errorf("discord returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
		}
	}
	return fmt.Errorf("giving up after retries: %w", lastErr)
}

func backoff(attempt int) time.Duration {
	return min(time.Second<<attempt, 8*time.Second)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func webhookID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "webhook"
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] // the id sits in front of the token
	}
	if parts[0] != "" {
		return parts[0]
	}
	return "webhook"
}
