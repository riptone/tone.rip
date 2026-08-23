// Package cv holds the CV content served over SSH.
//
// cv.json is generated from packages/content/src/cv.ts by
// scripts/generate-content.ts and committed, so `go build` works in a
// checkout with no Bun installed. It is embedded rather than read from disk
// so the binary is self-contained: dropping it on a host is the whole
// install.
//
// This is the long version of the CV. The website describes an organisation
// by what it does and stops there; here every role also carries its name and
// a few lines about what the work actually involved. Somebody who typed
// `ssh cv.no-tone.com` asked for that.
package cv

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed cv.json
var raw []byte

// Experience is one role.
type Experience struct {
	Role string `json:"role"`
	// Org describes what the organisation does. Always present; it is what
	// the website prints.
	Org string `json:"org"`
	// Company is its name. Empty for a role whose name has not been filled
	// in yet, in which case Org stands in - see Name.
	Company string `json:"company"`
	Period  string `json:"period"`
	Place   string `json:"place"`
	// Stack is the languages, frameworks and tools the role used.
	Stack   []string `json:"stack"`
	Bullets []string `json:"bullets"`
	// Detail is the longer version of the same work, rendered only here.
	Detail []string `json:"detail"`
}

// Name is what to call the organisation: its name when there is one, what it
// does otherwise. Never empty, so no caller has to decide what to show.
func (e Experience) Name() string {
	if e.Company != "" {
		return e.Company
	}
	return e.Org
}

type Education struct {
	Title   string   `json:"title"`
	Period  string   `json:"period"`
	Bullets []string `json:"bullets"`
}

// Certification is a certification and what it covers.
type Certification struct {
	Title string `json:"title"`
	Note  string `json:"note"`
}

// BestAt is a ranked "what I'm best at" row: a short key and its expansion.
type BestAt struct {
	K string `json:"k"`
	V string `json:"v"`
}

type SkillGroup struct {
	Label string   `json:"label"`
	Items []string `json:"items"`
}

// Contact is where to find the person the CV describes. Language-independent:
// every value is an address, and addresses do not translate.
type Contact struct {
	Web    string `json:"web"`
	Email  string `json:"email"`
	GitHub string `json:"github"`
	SSH    string `json:"ssh"`
}

// Labels are the section headings the website prints too, carried through
// from packages/content so renaming a section there renames it here. Anything
// the terminal alone says - key hints, "Stack", "In depth" - stays in
// internal/tui, which is the only thing that renders it.
type Labels struct {
	Experience     string `json:"experience"`
	Education      string `json:"education"`
	Certifications string `json:"certifications"`
	BestAt         string `json:"bestAt"`
	Skills         string `json:"skills"`
}

// complete reports whether every heading has a word in it. Checked at load
// rather than at render, so a section can never open under a blank title.
func (l Labels) complete() bool {
	return l.Experience != "" && l.Education != "" &&
		l.Certifications != "" && l.BestAt != "" && l.Skills != ""
}

// Content is the CV in one language.
type Content struct {
	Labels         Labels          `json:"labels"`
	BestAt         []BestAt        `json:"bestAt"`
	Experience     []Experience    `json:"experience"`
	Education      []Education     `json:"education"`
	Certifications []Certification `json:"certifications"`
	Skills         []SkillGroup    `json:"skills"`
	Spoken         []string        `json:"spoken"`
	Interests      []string        `json:"interests"`
}

// Document is the whole embedded CV: every language, plus the parts that are
// the same in all of them.
type Document struct {
	Langs   []string           `json:"langs"`
	Contact Contact            `json:"contact"`
	ByLang  map[string]Content `json:"byLang"`
}

// Content returns one language's CV, falling back to the first language so a
// caller can never render an empty document by asking for the wrong key.
func (d *Document) Content(lang string) Content {
	if content, ok := d.ByLang[lang]; ok {
		return content
	}
	return d.ByLang[d.Langs[0]]
}

// Prefer moves lang to the front of the language list, so the session opens
// in it. Returns a copy, because one loaded Document is shared by every
// session and a per-session preference must not reach the others.
func (d *Document) Prefer(lang string) *Document {
	if lang == "" {
		return d
	}
	for i, candidate := range d.Langs {
		if !strings.EqualFold(candidate, lang) {
			continue
		}
		if i == 0 {
			return d
		}
		ordered := make([]string, 0, len(d.Langs))
		ordered = append(ordered, candidate)
		ordered = append(ordered, d.Langs[:i]...)
		ordered = append(ordered, d.Langs[i+1:]...)
		return &Document{Langs: ordered, Contact: d.Contact, ByLang: d.ByLang}
	}
	return d
}

// Load parses the embedded CV.
//
// Returns an error rather than panicking on init so main can fail with a
// useful message instead of a stack trace, and so tests can assert the
// embedded document is well-formed.
func Load() (*Document, error) {
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse embedded cv.json: %w", err)
	}
	if err := doc.validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

// validate is what lets everything downstream stop checking.
//
// internal/tui renders a section heading straight from Labels and an index
// row straight from Experience; both are worth refusing at the door rather
// than defending against on every page.
func (d *Document) validate() error {
	if len(d.Langs) == 0 {
		return fmt.Errorf("cv.json declares no languages")
	}
	for _, lang := range d.Langs {
		content, ok := d.ByLang[lang]
		if !ok {
			return fmt.Errorf("cv.json declares language %q with no content", lang)
		}
		if len(content.Experience) == 0 {
			return fmt.Errorf("cv.json language %q has no experience entries", lang)
		}
		if !content.Labels.complete() {
			return fmt.Errorf(
				"cv.json language %q is missing section headings: %+v", lang, content.Labels)
		}
	}
	return nil
}
