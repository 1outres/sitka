package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/1outres/sitka/internal/events"
)

// watchPath is where a running gateway publishes what it routed.
const watchPath = "/_sitka/events"

// messagesPath is the endpoint a line does not need to name, because nearly
// every request goes there.
const messagesPath = "/v1/messages"

const (
	timeLayout    = "15:04:05"
	routeWidth    = 34
	durationWidth = 7
	shortIDWidth  = 8
)

func newWatchCommand(opts *options) *cobra.Command {
	var address string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Follow what a running gateway routes",
		Long: "Follow what a running gateway routes. Every request prints one line as it " +
			"finishes: the model the client asked for, the upstream that served it, how " +
			"long it took and what it cost in tokens.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if address == "" {
				cfg, err := opts.load()
				if err != nil {
					return err
				}
				address = cfg.Listen
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "sitka: watching %s\n", watchURL(address))
			return watch(cmd.Context(), address, cmd.OutOrStdout(), asJSON)
		},
	}
	cmd.Flags().StringVar(&address, "address", "",
		"address of the running gateway (default: the listen address of the config file)")
	cmd.Flags().BoolVar(&asJSON, "json", false,
		"print one JSON object per event instead of a formatted line")
	return cmd
}

// watch prints every event of the gateway at address until the stream ends.
func watch(ctx context.Context, address string, out io.Writer, asJSON bool) error {
	target := watchURL(address)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("watch %s: %w", target, err)
	}
	req.Header.Set("Accept", "text/event-stream")

	// The default client has no timeout, and the stream must stay open until
	// the gateway or the user ends it.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("reach the gateway at %s: %w", address, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the gateway at %s answered %s", address, resp.Status)
	}

	colors := paletteFor(out)
	decoder := events.NewDecoder(resp.Body)
	for {
		event, err := decoder.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("the gateway at %s closed the stream", address)
		}
		if err != nil {
			return err
		}
		if err := writeEvent(out, event, colors, asJSON); err != nil {
			return err
		}
	}
}

// watchURL turns a listen address into the URL of the event stream. An address
// that already names a scheme is kept, so a gateway reached another way can be
// watched too.
func watchURL(address string) string {
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	return strings.TrimRight(address, "/") + watchPath
}

func writeEvent(out io.Writer, event events.Event, colors palette, asJSON bool) error {
	line := formatEvent(event, colors)
	if asJSON {
		encoded, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("encode the event: %w", err)
		}
		line = string(encoded)
	}
	if _, err := fmt.Fprintln(out, line); err != nil {
		return fmt.Errorf("write the line: %w", err)
	}
	return nil
}

func formatEvent(event events.Event, colors palette) string {
	fields := []string{
		colors.paint(colors.dim, event.Time.Local().Format(timeLayout)),
		colors.paint(colors.forStatus(event.Status), strconv.Itoa(event.Status)),
		colors.paint(colors.forRoute(event), pad(routeOf(event), routeWidth)),
		colors.paint(colors.dim, padLeft(durationOf(event), durationWidth)),
	}
	if tokens := tokensOf(event); tokens != "" {
		fields = append(fields, tokens)
	}
	if event.StopReason != "" {
		fields = append(fields, colors.paint(colors.dim, event.StopReason))
	}
	if who := callerOf(event); who != "" {
		fields = append(fields, colors.paint(colors.dim, who))
	}
	if event.Model != "" && event.Path != messagesPath {
		fields = append(fields, colors.paint(colors.dim, event.Path))
	}
	return strings.Join(fields, " ")
}

// routeOf names where the request went. A request that reached no model shows
// what it asked for instead.
func routeOf(event events.Event) string {
	if event.Model == "" {
		return event.Method + " " + event.Path
	}
	if event.UpstreamModel == "" || event.UpstreamModel == event.Model {
		return event.Model + " → " + event.Provider
	}
	return event.Model + " → " + event.Provider + "/" + event.UpstreamModel
}

func durationOf(event events.Event) string {
	took := time.Duration(event.DurationMS) * time.Millisecond
	if took == 0 {
		// The column counts milliseconds, so a request that took less than one
		// must not read as whole seconds.
		return "0ms"
	}
	if took >= time.Second {
		return took.Round(100 * time.Millisecond).String()
	}
	return took.String()
}

func tokensOf(event events.Event) string {
	if event.Usage == nil {
		return ""
	}
	counts := []string{
		"in=" + formatTokens(event.Usage.InputTokens),
		"out=" + formatTokens(event.Usage.OutputTokens),
	}
	if event.Usage.CacheReadTokens > 0 {
		counts = append(counts, "cache_r="+formatTokens(event.Usage.CacheReadTokens))
	}
	if event.Usage.CacheCreationTokens > 0 {
		counts = append(counts, "cache_w="+formatTokens(event.Usage.CacheCreationTokens))
	}
	return strings.Join(counts, " ")
}

func formatTokens(count int) string {
	switch {
	case count >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(count)/1_000_000)
	case count >= 1_000:
		return fmt.Sprintf("%.1fk", float64(count)/1_000)
	default:
		return strconv.Itoa(count)
	}
}

// callerOf names the subagent that made the request, or the session when the
// main conversation made it.
func callerOf(event events.Event) string {
	if event.Agent != "" {
		return "agent=" + shortID(event.Agent)
	}
	if event.Session != "" {
		return "session=" + shortID(event.Session)
	}
	return ""
}

func shortID(id string) string {
	if len(id) <= shortIDWidth {
		return id
	}
	return id[:shortIDWidth]
}

func pad(text string, width int) string {
	if missing := width - utf8.RuneCountInString(text); missing > 0 {
		return text + strings.Repeat(" ", missing)
	}
	return text
}

func padLeft(text string, width int) string {
	if missing := width - utf8.RuneCountInString(text); missing > 0 {
		return strings.Repeat(" ", missing) + text
	}
	return text
}

// palette holds the escape codes of one output style. The plain palette leaves
// every code empty, so the same formatting serves a pipe and a terminal.
type palette struct {
	reset string
	dim   string
	route string
	ok    string
	warn  string
	bad   string
}

var plainPalette = palette{}

var colorPalette = palette{
	reset: "\x1b[0m",
	dim:   "\x1b[2m",
	route: "\x1b[36m",
	ok:    "\x1b[32m",
	warn:  "\x1b[33m",
	bad:   "\x1b[31m",
}

// paletteFor colors the output only when it goes to a terminal that wants it.
func paletteFor(out io.Writer) palette {
	if os.Getenv("NO_COLOR") != "" {
		return plainPalette
	}
	file, ok := out.(*os.File)
	if !ok {
		return plainPalette
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return plainPalette
	}
	return colorPalette
}

func (p palette) paint(color, text string) string {
	if color == "" {
		return text
	}
	return color + text + p.reset
}

func (p palette) forStatus(status int) string {
	switch {
	case status >= http.StatusInternalServerError:
		return p.bad
	case status >= http.StatusBadRequest:
		return p.warn
	default:
		return p.ok
	}
}

func (p palette) forRoute(event events.Event) string {
	if event.Model == "" {
		return p.dim
	}
	return p.route
}
