package editor

import (
	"regexp"
	"strings"
	"testing"
)

// pageSource returns the embedded editor page.
func pageSource(t *testing.T) string {
	t.Helper()
	data, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatalf("reading the embedded page: %v", err)
	}
	return string(data)
}

// styleBlock returns the page's CSS.
func styleBlock(t *testing.T) string {
	t.Helper()
	page := pageSource(t)
	start := strings.Index(page, "<style>")
	end := strings.Index(page, "</style>")
	if start < 0 || end < 0 {
		t.Fatal("no <style> block in the editor page")
	}
	return page[start+len("<style>") : end]
}

// The bug this guards against hid the entire application on the first
// keystroke: the class toggled onto <body> to mark unsaved changes had the same
// name as the indicator span's class, and that class carried `display: none`.
// So `body` matched it and hid itself.
//
// The general rule is that a class used as a state flag on <body> must never be
// a class that any rule hides. This finds the flag in the script and checks it.
func TestBodyStateClassesAreNotHiddenByCSS(t *testing.T) {
	page := pageSource(t)
	css := styleBlock(t)

	toggles := regexp.MustCompile(`document\.body\.classList\.(?:toggle|add)\('([a-zA-Z0-9_-]+)'`).
		FindAllStringSubmatch(page, -1)
	if len(toggles) == 0 {
		t.Fatal("found no class toggled onto <body>; this test has stopped matching the script")
	}

	for _, match := range toggles {
		class := match[1]
		// A rule whose selector is exactly this class — no element part, no
		// descendant — matches <body> as readily as anything else.
		bare := regexp.MustCompile(`(?m)^\s*\.` + regexp.QuoteMeta(class) + `\s*\{([^}]*)\}`)
		for _, rule := range bare.FindAllStringSubmatch(css, -1) {
			if strings.Contains(rule[1], "display: none") || strings.Contains(rule[1], "display:none") {
				t.Errorf("`.%s` is toggled onto <body> and also carries display:none, "+
					"so the first keystroke hides the whole page:\n  %s", class, strings.TrimSpace(rule[0]))
			}
		}
	}
}

// The highlighted <pre> sits behind the textarea and the two must break lines
// identically, or the caret drifts away from the text under it. `wrap="off"` is
// what makes a textarea agree with `white-space: pre`.
func TestTextareaWrapsLikeTheHighlightLayer(t *testing.T) {
	page := pageSource(t)
	if !regexp.MustCompile(`<textarea[^>]*\bwrap="off"`).MatchString(page) {
		t.Error(`the textarea needs wrap="off" to match the highlight layer's white-space: pre`)
	}

	css := styleBlock(t)
	shared := regexp.MustCompile(`\.code pre, \.code textarea \{([^}]*)\}`).FindStringSubmatch(css)
	if shared == nil {
		t.Fatal("the pre and the textarea no longer share one rule; every metric that affects " +
			"glyph position must be declared once for both, or they will drift apart")
	}
	// Anything that moves a glyph has to be in the shared rule.
	for _, property := range []string{"padding", "font-family", "font-size", "line-height", "white-space", "tab-size"} {
		if !strings.Contains(shared[1], property) {
			t.Errorf("%q is not in the shared pre/textarea rule, so the two layers can disagree about it", property)
		}
	}
}

// One scroller. The wrapper used to scroll as well, which gave the gutter and
// the highlight layer a second, unsynchronised offset.
func TestOnlyTheTextareaScrolls(t *testing.T) {
	css := styleBlock(t)
	tests := []struct {
		selector string
		want     string
	}{
		{`.editor-wrap`, "overflow: hidden"},
		{`.code pre`, "overflow: hidden"},
		{`.gutter`, "overflow: hidden"},
		{`.code textarea`, "overflow: auto"},
	}
	for _, tt := range tests {
		// Anchored to the start of a line so this finds the standalone rule
		// rather than the shared `.code pre, .code textarea` one, whose
		// selector contains the same text.
		rule := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(tt.selector) + `\s*\{([^}]*)\}`).
			FindStringSubmatch(css)
		if rule == nil {
			t.Errorf("no rule for %s", tt.selector)
			continue
		}
		if !strings.Contains(rule[1], tt.want) {
			t.Errorf("%s should declare %q so the textarea is the only scroller; got:\n  %s",
				tt.selector, tt.want, strings.TrimSpace(rule[1]))
		}
	}
}

// Whatever scrolls has to be followed by the layers behind it.
func TestScrollIsSynchronised(t *testing.T) {
	page := pageSource(t)
	for _, needed := range []string{"highlightPre.scrollTop", "highlightPre.scrollLeft", "gutter.scrollTop"} {
		if !strings.Contains(page, needed) {
			t.Errorf("the scroll handler no longer syncs %s", needed)
		}
	}
	if !strings.Contains(page, "source.addEventListener('scroll', syncScroll)") {
		t.Error("the textarea's scroll event is not wired to the sync")
	}
}

// The page is a Go template, and a broken action would only show up at runtime.
func TestPageTemplateParses(t *testing.T) {
	if _, err := New(Options{Path: "doc.pulp"}); err != nil {
		t.Fatalf("the embedded page does not parse as a template: %v", err)
	}
}
