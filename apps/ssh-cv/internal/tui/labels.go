package tui

import "github.com/riptone/tone.rip/apps/ssh-cv/internal/cv"

// The words around the CV, per language.
//
// Only the ones this surface alone says: key hints, "Stack", "In depth", the
// colophon. The five headings the website prints as well - Experience,
// Education, Certifications, Best at, Skills - come from packages/content via
// cv.json, so renaming a section there renames it in both places rather than
// leaving the terminal quietly disagreeing with the site.
//
// They live here rather than in the content module because they are chrome,
// not CV - but they still have to switch with it. The old footer said
// "p língua" in an English session, which is the kind of small wrongness that
// makes everything next to it look accidental.
type labels struct {
	// The window's own name, right-aligned on the title bar.
	app string

	// The index's second group heading. The first is the experience label.
	more string

	// Section names, used both in the index and as the page title. The first
	// five arrive from the CV itself - see labelsFor.
	experience     string
	bestAt         string
	education      string
	certifications string
	skills         string
	personal       string
	contact        string

	// Headings inside a section.
	stack     string
	work      string
	depth     string
	spoken    string
	interests string
	colophon  string

	// Key hints and the line counter.
	move     string
	open     string
	back     string
	section  string
	scroll   string
	language string
	quit     string
	of       string
}

var labelsByLang = map[string]labels{
	"en": {
		app:       "cv",
		more:      "More",
		personal:  "Personal",
		contact:   "Contact",
		stack:     "Stack",
		work:      "The work",
		depth:     "In depth",
		spoken:    "Languages",
		interests: "Interests",
		colophon:  "This session is a Go program: wish, bubbletea, lipgloss.",
		move:      "move",
		open:      "open",
		back:      "back",
		section:   "section",
		scroll:    "scroll",
		language:  "lang",
		quit:      "quit",
		of:        "of",
	},
	"pt": {
		app:       "cv",
		more:      "Mais",
		personal:  "Pessoal",
		contact:   "Contacto",
		stack:     "Stack",
		work:      "O trabalho",
		depth:     "Em detalhe",
		spoken:    "Línguas",
		interests: "Interesses",
		colophon:  "Esta sessão é um programa em Go: wish, bubbletea, lipgloss.",
		move:      "mover",
		open:      "abrir",
		back:      "voltar",
		section:   "secção",
		scroll:    "scroll",
		language:  "língua",
		quit:      "sair",
		of:        "de",
	},
}

// labelsFor assembles one language's words: this file's for the chrome, the
// CV's own for the five headings it shares with the website.
//
// The chrome falls back to English for a language the CV gains before this
// table does - a session in the wrong chrome is recoverable, a session of
// blank section names is not. The headings need no fallback: cv.Load refuses
// a document that is missing any of them.
func labelsFor(lang string, shared cv.Labels) labels {
	l, ok := labelsByLang[lang]
	if !ok {
		l = labelsByLang["en"]
	}
	l.experience = shared.Experience
	l.education = shared.Education
	l.certifications = shared.Certifications
	l.bestAt = shared.BestAt
	l.skills = shared.Skills
	return l
}
