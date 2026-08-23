package cv

import "testing"

func TestLoadEmbedded(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(doc.Langs) < 2 {
		t.Fatalf("expected at least two languages, got %v", doc.Langs)
	}
	for _, lang := range doc.Langs {
		content := doc.Content(lang)
		if len(content.Experience) == 0 {
			t.Errorf("%s: no experience", lang)
		}
		if len(content.Skills) == 0 {
			t.Errorf("%s: no skills", lang)
		}
		if len(content.BestAt) == 0 {
			t.Errorf("%s: no bestAt", lang)
		}
		if len(content.Certifications) == 0 {
			t.Errorf("%s: no certifications", lang)
		}
		for _, exp := range content.Experience {
			if exp.Role == "" || exp.Period == "" {
				t.Errorf("%s: experience entry missing role or period: %+v", lang, exp)
			}
			// The stack is why a reader opens a role, so an entry without one
			// is a content bug rather than a rendering choice.
			if len(exp.Stack) == 0 {
				t.Errorf("%s: %q lists no stack", lang, exp.Role)
			}
		}
	}
}

// The section headings come from packages/content now, and the TUI renders
// them without checking - so a document that lost them has to be refused
// here, at the one boundary where the JSON becomes Go.
func TestValidateRefusesAMissingHeading(t *testing.T) {
	whole := Labels{
		Experience:     "Experience",
		Education:      "Education",
		Certifications: "Certifications",
		BestAt:         "Best at",
		Skills:         "Skills",
	}
	build := func(labels Labels) *Document {
		return &Document{
			Langs: []string{"en"},
			ByLang: map[string]Content{"en": {
				Labels:     labels,
				Experience: []Experience{{Role: "Engineer", Period: "now"}},
			}},
		}
	}

	if err := build(whole).validate(); err != nil {
		t.Fatalf("a complete document was refused: %v", err)
	}

	// One at a time, because a check that only fires when every heading is
	// missing would pass a document with four of five.
	missing := map[string]Labels{
		"experience":     {Education: "e", Certifications: "c", BestAt: "b", Skills: "s"},
		"education":      {Experience: "x", Certifications: "c", BestAt: "b", Skills: "s"},
		"certifications": {Experience: "x", Education: "e", BestAt: "b", Skills: "s"},
		"bestAt":         {Experience: "x", Education: "e", Certifications: "c", Skills: "s"},
		"skills":         {Experience: "x", Education: "e", Certifications: "c", BestAt: "b"},
	}
	for name, labels := range missing {
		if err := build(labels).validate(); err == nil {
			t.Errorf("a document with no %q heading was accepted", name)
		}
	}
}

// The headings really are in the embedded document, in every language.
func TestEmbeddedHeadingsArePresent(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, lang := range doc.Langs {
		if !doc.Content(lang).Labels.complete() {
			t.Errorf("%s: incomplete headings %+v", lang, doc.Content(lang).Labels)
		}
	}
}

// The contact page is four rows and a colophon; a missing value would drop a
// row silently, and the generator promises it cannot happen.
func TestContactIsComplete(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for name, value := range map[string]string{
		"web":    doc.Contact.Web,
		"email":  doc.Contact.Email,
		"github": doc.Contact.GitHub,
		"ssh":    doc.Contact.SSH,
	} {
		if value == "" {
			t.Errorf("contact.%s is empty", name)
		}
	}
}

// The CV is generated from the site's content module; if the two ever drift
// out of sync this is the test that should notice, because the languages
// must stay symmetric.
func TestLanguagesAreSymmetric(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	first := doc.Content(doc.Langs[0])
	for _, lang := range doc.Langs[1:] {
		other := doc.Content(lang)
		for _, check := range []struct {
			what        string
			left, right int
		}{
			{"experience", len(other.Experience), len(first.Experience)},
			{"education", len(other.Education), len(first.Education)},
			{"certifications", len(other.Certifications), len(first.Certifications)},
			{"skills", len(other.Skills), len(first.Skills)},
		} {
			if check.left != check.right {
				t.Errorf("%s has %d %s entries, %s has %d",
					lang, check.left, check.what, doc.Langs[0], check.right)
			}
		}
		// A stack is tech names, which are not translated - so the two
		// languages must list the same ones, or one surface is wrong.
		for i := range other.Experience {
			if i >= len(first.Experience) {
				break
			}
			if len(other.Experience[i].Stack) != len(first.Experience[i].Stack) {
				t.Errorf("%s role %d lists %d stack entries, %s lists %d",
					lang, i, len(other.Experience[i].Stack),
					doc.Langs[0], len(first.Experience[i].Stack))
			}
		}
	}
}

// Name is what every surface calls the organisation, so its fallback is load
// bearing: every role is waiting for a company name right now, and none of
// them may render as a blank.
func TestNameFallsBackToWhatTheOrgDoes(t *testing.T) {
	named := Experience{Org: "cloud management provider", Company: "Acme"}
	if got := named.Name(); got != "Acme" {
		t.Errorf("Name() = %q, want the company", got)
	}
	unnamed := Experience{Org: "cloud management provider"}
	if got := unnamed.Name(); got != "cloud management provider" {
		t.Errorf("Name() = %q, want the description", got)
	}
}

func TestPreferMovesALanguageToTheFront(t *testing.T) {
	doc := &Document{
		Langs:  []string{"en", "pt"},
		ByLang: map[string]Content{"en": {}, "pt": {}},
	}

	if got := doc.Prefer("PT").Langs; got[0] != "pt" || len(got) != 2 {
		t.Errorf("Prefer(\"PT\").Langs = %v, want pt first", got)
	}
	// One document is shared by every session, so a preference must be a copy.
	if doc.Langs[0] != "en" {
		t.Errorf("Prefer mutated the shared document: %v", doc.Langs)
	}
	for _, requested := range []string{"", "en", "klingon"} {
		if got := doc.Prefer(requested).Langs; got[0] != "en" {
			t.Errorf("Prefer(%q).Langs = %v, want the default order", requested, got)
		}
	}
}

// Content answers with something renderable whatever it is asked for: a
// session that asked for the wrong key should see the CV, not a blank frame.
func TestContentFallsBackToTheFirstLanguage(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(doc.Content("klingon").Experience) == 0 {
		t.Error("Content() returned an empty CV for an unknown language")
	}
}
