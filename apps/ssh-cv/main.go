// Command ssh-cv serves a CV over SSH.
//
//	ssh cv.tone.rip
//
// It is the long version of the CV the website prints: the same content
// module, but with the company names and the per-role detail that /cv leaves
// out. Anyone may read it - typing that command is the only credential it
// asks for.
//
// One caveat shapes the deployment: SSH has no SNI. The client never tells
// the server which hostname it dialled, so every name pointing at this
// address is the same connection here. See README.md.
package main

import (
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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
	"github.com/charmbracelet/wish/recover"
	"github.com/muesli/termenv"

	"github.com/riptone/tone.rip/packages/gotui"
	gossh "golang.org/x/crypto/ssh"

	"github.com/riptone/tone.rip/apps/ssh-cv/internal/authz"
	"github.com/riptone/tone.rip/apps/ssh-cv/internal/cv"
	"github.com/riptone/tone.rip/apps/ssh-cv/internal/tui"
	"github.com/riptone/tone.rip/apps/ssh-cv/internal/version"
)

// contextKey is unexported so nothing outside this package can collide with
// it in the session context.
type contextKey string

const grantKey contextKey = "tone.grant"
const fingerprintKey contextKey = "tone.fingerprint"

type config struct {
	showVersion    bool
	preview        bool
	addr           string
	hostKeyPath    string
	authorizeURL   string
	authorizeToken string
	authorizedKeys string
	idleTimeout    time.Duration
	maxTimeout     time.Duration
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func parseFlags() config {
	var cfg config
	// Deliberately prints one bare token and exits: scripts/install.sh reads
	// this to decide whether the box is already up to date, and compares it
	// against a release tag with a plain string test.
	flag.BoolVar(&cfg.showVersion, "version", false,
		"print the version and exit")
	flag.BoolVar(&cfg.preview, "preview", false,
		"render the CV in this terminal instead of serving it over SSH")
	flag.StringVar(&cfg.addr, "addr", envOr("SSH_ADDR", ":22"),
		"address to listen on")
	flag.StringVar(&cfg.hostKeyPath, "host-key", envOr("SSH_HOST_KEY", ".ssh/ssh_cv_ed25519"),
		"path to the host key; generated on first run if absent")
	flag.StringVar(&cfg.authorizeURL, "authorize-url", os.Getenv("SSH_AUTHORIZE_URL"),
		"apps/api endpoint that resolves a key fingerprint to a grant")
	flag.StringVar(&cfg.authorizedKeys, "authorized-keys", os.Getenv("SSH_AUTHORIZED_KEYS_FILE"),
		"local authorized_keys file to use instead of the API (for local dev)")
	flag.DurationVar(&cfg.idleTimeout, "idle-timeout", 5*time.Minute,
		"disconnect a session after this long with no activity")
	flag.DurationVar(&cfg.maxTimeout, "max-timeout", 30*time.Minute,
		"hard cap on session duration")
	flag.Parse()

	// Never a flag: a token on the command line is visible in `ps` to every
	// user on the box.
	cfg.authorizeToken = os.Getenv("SSH_AUTHORIZE_TOKEN")
	return cfg
}

// buildAuthorizer resolves where key labels come from.
//
// Nothing in the CV is gated on the answer - it is public, the same way the
// website is - so a server with no allowlist configured is fully functional.
// What a recognised key buys is its label in the footer, and the scopes it
// carries are there for whatever asks for one next.
func buildAuthorizer(cfg config) (authz.Authorizer, string, error) {
	if cfg.authorizedKeys != "" {
		data, err := os.ReadFile(cfg.authorizedKeys)
		if err != nil {
			return nil, "", fmt.Errorf("read %s: %w", cfg.authorizedKeys, err)
		}
		grants, err := authz.ParseAuthorizedKeys(data)
		if err != nil {
			return nil, "", err
		}
		return authz.StaticAuthorizer{Grants: grants},
			fmt.Sprintf("local authorized_keys (%d keys)", len(grants)), nil
	}

	if cfg.authorizeURL != "" {
		if cfg.authorizeToken == "" {
			// Without the token the API cannot tell this server from anyone
			// else who found the endpoint, which turns the allowlist into an
			// oracle. Refuse rather than run in a weaker mode than intended.
			return nil, "", errors.New(
				"SSH_AUTHORIZE_TOKEN is required when --authorize-url is set")
		}
		return &authz.APIAuthorizer{
			Endpoint: cfg.authorizeURL,
			Token:    cfg.authorizeToken,
			Client:   &http.Client{Timeout: 5 * time.Second},
		}, "apps/api at " + cfg.authorizeURL, nil
	}

	return authz.Denier{}, "none - the CV is public", nil
}

func main() {
	cfg := parseFlags()

	// Answered before anything can fail. The updater calls this on a binary
	// it has just downloaded and not yet trusted, so it must not depend on a
	// loadable CV, a writable host key, or a free port.
	if cfg.showVersion {
		fmt.Println(version.Short())
		return
	}

	doc, err := cv.Load()
	if err != nil {
		log.Fatalf("ssh-cv: %v", err)
	}

	// `bun run preview`. The TUI is the product here, and everything about a
	// session except the transport is local: same model, same content, same
	// keys. Checking a spacing change should not cost a host key, an
	// allowlist and a second terminal.
	if cfg.preview {
		if err := runPreview(doc); err != nil {
			log.Fatalf("ssh-cv: preview: %v", err)
		}
		return
	}

	authorizer, source, err := buildAuthorizer(cfg)
	if err != nil {
		log.Fatalf("ssh-cv: %v", err)
	}

	server, err := wish.NewServer(
		wish.WithAddress(cfg.addr),
		wish.WithHostKeyPath(cfg.hostKeyPath),
		wish.WithIdleTimeout(cfg.idleTimeout),
		wish.WithMaxTimeout(cfg.maxTimeout),

		// Accept every key, then decide what it is called. Refusing unknown
		// keys at the handshake would make the CV private, which defeats the
		// point - the whole appeal is that `ssh cv.tone.rip` just works.
		wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			fingerprint := authz.Fingerprint(key)
			ctx.SetValue(fingerprintKey, fingerprint)
			ctx.SetValue(grantKey, authorizer.Authorize(ctx, fingerprint))
			return true
		}),
		// Keyboard-interactive with no prompts lets a client that offers no
		// key connect anyway, and get the CV.
		wish.WithKeyboardInteractiveAuth(func(ssh.Context, gossh.KeyboardInteractiveChallenge) bool {
			return true
		}),

		// Composed first to last, executed last to first: logging wraps
		// activeterm, which wraps recover, which wraps the program. So
		//
		//   - every session is logged, including the ones rejected below it;
		//   - sessions with no PTY (`ssh host command`, scp, port forwards)
		//     are refused, because a Bubble Tea program needs a terminal and
		//     without this they hang instead of failing;
		//   - the client's terminal is asked to make its own default colours
		//     black and white, and told to put them back when the session
		//     ends;
		//   - a panic in one session is logged and ends that session, rather
		//     than unwinding into the server and taking every other
		//     connection down with it.
		wish.WithMiddleware(
			recover.Middleware(
				bubbletea.Middleware(func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
					return newSession(s, doc)
				}),
			),
			terminalColours,
			activeterm.Middleware(),
			logging.Middleware(),
		),
	)
	if err != nil {
		log.Fatalf("ssh-cv: build server: %v", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// First line in the journal after a restart, so `journalctl -u ssh-cv`
	// answers "did the update actually land" without running anything.
	log.Printf("ssh-cv: version %s", version.String())
	log.Printf("ssh-cv: listening on %s", cfg.addr)
	log.Printf("ssh-cv: key labels from: %s", source)

	go func() {
		if err := server.ListenAndServe(); err != nil &&
			!errors.Is(err, ssh.ErrServerClosed) {
			log.Fatalf("ssh-cv: serve: %v", err)
		}
	}()

	<-done
	log.Print("ssh-cv: shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil &&
		!errors.Is(err, ssh.ErrServerClosed) {
		log.Printf("ssh-cv: shutdown: %v", err)
	}
}

// runPreview renders one session on this terminal.
//
// No grant is passed, so what you see is what a stranger sees - the common
// case, and the one worth looking at while changing the layout.
func runPreview(doc *cv.Document) error {
	// The same request a session makes of its client, so the preview looks
	// like the thing it is previewing.
	defer gotui.PaintTerminal(os.Stdout)()

	program := tea.NewProgram(
		tui.New(tui.Config{Doc: doc}),
		tea.WithAltScreen(),
	)
	_, err := program.Run()
	return err
}

// terminalColours asks the client's terminal to make its own defaults black
// for the duration of a session, and puts them back afterwards.
//
// The sequences and the reasoning live in gotui.PaintTerminal, shared with
// apps/doti - which drew the same black card and, having none of this, let the
// reader's theme show through the emulator's padding.
//
// Deferred rather than restored after next(): a session that ends with the
// client vanishing still runs this, and a terminal left black after the CV has
// gone would be the CV's fault.
func terminalColours(next ssh.Handler) ssh.Handler {
	return func(s ssh.Session) {
		if _, _, ok := s.Pty(); ok {
			defer gotui.PaintTerminal(s)()
		}
		next(s)
	}
}

// sessionRenderer builds the renderer a session paints with.
//
// **Why it cannot be lipgloss's default one.** That renderer is bound to
// *this process's* stdout, which under systemd is a pipe rather than a
// terminal - so its colour profile resolves to Ascii and every colour is
// silently stripped from every session. That was a real bug: a window whose
// three buttons came out grey, on a CV whose whole point is that it looks like
// a window. Colour has to be decided per session, from the client's terminal.
//
// **Why not wish's MakeRenderer.** It does the above correctly, and then also
// queries the client for its background colour - which blocks for up to two
// seconds on any terminal that does not answer, eating the reader's first
// keystrokes while it waits. The palette here is fixed (see theme.go), so the
// answer would be thrown away. Nothing is gained for the stall.
//
// **Why the floor.** TERM under-reports constantly over SSH: ssh forwards it
// and little else, and plain `xterm` means sixteen colours to termenv. That
// policy - raise anything below 256, take `dumb` at its word - is
// gotui.ClampProfile, shared with apps/doti, whose local terminal
// under-reports for its own reasons (a multiplexer claiming `screen`).
//
// What stays here is the half that is genuinely about a session: which writer
// to paint, and whose environment to read TERM out of.
func sessionRenderer(s ssh.Session) *lipgloss.Renderer {
	pty, _, ok := s.Pty()
	if !ok {
		return gotui.ClampProfile(lipgloss.NewRenderer(s), "")
	}

	out := io.Writer(s)
	if pty.Slave != nil {
		out = pty.Slave
	}
	r := lipgloss.NewRenderer(out,
		termenv.WithEnvironment(sessionEnv(append(s.Environ(), "TERM="+pty.Term))),
		// The writer is the client's terminal, which this process cannot
		// isatty(); without this, detection declines to look at all.
		termenv.WithUnsafe(),
		termenv.WithColorCache(true),
	)
	return gotui.ClampProfile(r, pty.Term)
}

// sessionEnv is the client's environment, for termenv to read TERM and
// COLORTERM out of.
type sessionEnv []string

func (e sessionEnv) Environ() []string { return e }

func (e sessionEnv) Getenv(key string) string {
	for _, entry := range e {
		if value, found := strings.CutPrefix(entry, key+"="); found {
			return value
		}
	}
	return ""
}

func newSession(s ssh.Session, doc *cv.Document) (tea.Model, []tea.ProgramOption) {
	pty, _, _ := s.Pty()

	grant, _ := s.Context().Value(grantKey).(authz.Grant)
	fingerprint, _ := s.Context().Value(fingerprintKey).(string)

	// The SSH username is not an identity here - anyone can type anything -
	// but `ssh pt@cv.tone.rip` is a pleasant way to land in Portuguese, so
	// it is honoured as a preference and nothing more. Prefer returns a copy,
	// so one session's language never reaches another's.
	model := tui.New(tui.Config{
		Doc:         doc.Prefer(s.User()),
		Grant:       grant,
		Width:       pty.Window.Width,
		Height:      pty.Window.Height,
		Fingerprint: fingerprint,
		Renderer:    sessionRenderer(s),
	})
	return model, []tea.ProgramOption{tea.WithAltScreen()}
}
