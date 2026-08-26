package vars

import (
	"strings"
	"testing"

	"github.com/jclement/treekillbot/internal/pulp"
)

// fakeEnv is an injected environment, so these tests never touch the real one
// and can run in parallel with everything else.
func fakeEnv(pairs map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := pairs[name]
		return v, ok
	}
}

func TestPrecedenceLadder(t *testing.T) {
	// Lowest to highest: built-in, theme, environment, vars block, vars file,
	// --var, and a lexical binding above the lot.
	s := NewScope(date(t, "2026-08-24"), Options{})

	// A definition at a lower layer never displaces a higher one, so the
	// compiler may process its sources in any order.
	s.Define("owner", NewString("cli"), LayerCLI)
	s.Define("owner", NewString("theme"), LayerTheme)
	if got := clean(t, s, "{owner}"); got != "cli" {
		t.Fatalf("a theme definition displaced --var: got %q", got)
	}

	s = NewScope(date(t, "2026-08-24"), Options{})
	steps := []struct {
		name  string
		layer Layer
		want  string
	}{
		{"theme", LayerTheme, "theme"},
		{"environment", LayerEnv, "environment"},
		{"vars block", LayerDocument, "vars block"},
		{"vars file", LayerVarsFile, "vars file"},
		{"--var", LayerCLI, "--var"},
	}
	for _, step := range steps {
		s.Define("owner", NewString(step.want), step.layer)
		if got := clean(t, s, "{owner}"); got != step.want {
			t.Fatalf("after defining at %s, {owner} = %q, want %q", step.name, got, step.want)
		}
	}

	s.Push()
	s.Bind("owner", NewString("let"))
	if got := clean(t, s, "{owner}"); got != "let" {
		t.Fatalf("a lexical binding lost to --var: got %q", got)
	}
	s.Pop()
	if got := clean(t, s, "{owner}"); got != "--var" {
		t.Fatalf("after Pop, {owner} = %q, want the --var value back", got)
	}
}

func TestDocumentValuesBeatBuiltins(t *testing.T) {
	// Built-ins are the bottom of the ladder: a document that wants its own
	// `today` gets it.
	s := NewScope(date(t, "2026-08-24"), Options{})
	if got := clean(t, s, "{today}"); got != "2026-08-24" {
		t.Fatalf("{today} = %q", got)
	}
	s.Define("today", NewString("whenever"), LayerDocument)
	if got := clean(t, s, "{today}"); got != "whenever" {
		t.Fatalf("{today} = %q, want the vars-block value", got)
	}
}

func TestLetDoesNotLeakToSiblings(t *testing.T) {
	s := NewScope(date(t, "2026-08-24"), Options{})

	// section one
	s.Push()
	s.Bind("column-w", NewString("30%"))
	if got := clean(t, s, "{column-w}"); got != "30%" {
		t.Fatalf("inside the section, {column-w} = %q", got)
	}
	s.Pop()

	// section two, a sibling
	s.Push()
	defer s.Pop()
	out, diags := expand(t, s, "{column-w}")
	if out != "" || len(diags) == 0 {
		t.Fatalf("the sibling saw %q with %d diagnostics; a `let` must not leak", out, len(diags))
	}
	if diags[0].Code != "E210" {
		t.Fatalf("got %s, want E210", diags[0].Code)
	}
}

func TestInnerBindingShadowsAnOuterOneOfTheSameName(t *testing.T) {
	s := NewScope(date(t, "2026-08-24"), Options{})
	s.Push()
	s.Bind("day", NewString("outer"))
	s.Push()
	s.Bind("day", NewString("inner"))
	if got := clean(t, s, "{day}"); got != "inner" {
		t.Fatalf("{day} = %q, want the inner loop's binding", got)
	}
	s.Pop()
	if got := clean(t, s, "{day}"); got != "outer" {
		t.Fatalf("{day} = %q, want the outer binding back", got)
	}
	s.Pop()
	if s.Depth() != 0 {
		t.Fatalf("Depth() = %d after unwinding, want 0", s.Depth())
	}
}

func TestPopWithoutPushPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Pop on an empty stack did not panic; an unbalanced compiler would go unnoticed")
		}
	}()
	NewScope(date(t, "2026-08-24"), Options{}).Pop()
}

func TestEnvironmentIsDeclaredNeverAmbient(t *testing.T) {
	env := fakeEnv(map[string]string{"HOME": "/Users/jsc", "USER": "jsc", "AWS_SECRET": "hunter2"})

	t.Run("an undeclared variable is refused", func(t *testing.T) {
		s := NewScope(date(t, "2026-08-24"), Options{Environ: env})
		out, diags := expand(t, s, "{env.AWS_SECRET}")
		if out != "" {
			t.Errorf("output %q, want nothing", out)
		}
		if len(diags) == 0 || diags[0].Code != "E212" {
			t.Fatalf("got %v, want E212", diags)
		}
	})

	t.Run("a fallback does not make it legal", func(t *testing.T) {
		s := NewScope(date(t, "2026-08-24"), Options{Environ: env})
		_, diags := expand(t, s, "{env.AWS_SECRET|nothing}")
		if len(diags) == 0 || diags[0].Code != "E212" {
			t.Fatalf("got %v, want E212 even with a fallback", diags)
		}
	})

	t.Run("--allow-undefined does not downgrade the refusal", func(t *testing.T) {
		s := NewScope(date(t, "2026-08-24"), Options{Environ: env, AllowUndefined: true})
		_, diags := expand(t, s, "{env.AWS_SECRET}")
		if len(diags) == 0 || diags[0].Severity != pulp.SeverityError {
			t.Fatalf("got %v, want an error", diags)
		}
	})

	t.Run("a declared variable resolves", func(t *testing.T) {
		s := NewScope(date(t, "2026-08-24"), Options{Environ: env})
		s.PermitEnv("HOME")
		if got := clean(t, s, "{env.HOME}"); got != "/Users/jsc" {
			t.Fatalf("{env.HOME} = %q", got)
		}
	})

	t.Run("declared but unset falls back", func(t *testing.T) {
		s := NewScope(date(t, "2026-08-24"), Options{Environ: env})
		s.PermitEnv("EDITOR")
		if got := clean(t, s, "{env.EDITOR|vi}"); got != "vi" {
			t.Fatalf("{env.EDITOR|vi} = %q", got)
		}
		_, diags := expand(t, s, "{env.EDITOR}")
		if len(diags) == 0 || diags[0].Code != "E210" {
			t.Fatalf("got %v, want E210", diags)
		}
	})

	t.Run("--unsafe-env opens everything", func(t *testing.T) {
		s := NewScope(date(t, "2026-08-24"), Options{Environ: env, UnsafeEnv: true})
		if got := clean(t, s, "{env.AWS_SECRET}"); got != "hunter2" {
			t.Fatalf("{env.AWS_SECRET} = %q", got)
		}
	})

	t.Run("an unqualified name never reaches the environment", func(t *testing.T) {
		s := NewScope(date(t, "2026-08-24"), Options{Environ: env, UnsafeEnv: true})
		out, diags := expand(t, s, "{USER}")
		if out != "" || len(diags) == 0 || diags[0].Code != "E210" {
			t.Fatalf("{USER} = %q with %v; it must not pick up the shell", out, diags)
		}
	})
}

func TestDeclareFillsRequiredParameters(t *testing.T) {
	span := pulp.Span{Start: 10, End: 15}

	t.Run("from --var", func(t *testing.T) {
		s := NewScope(date(t, "2026-08-24"), Options{Environ: fakeEnv(nil)})
		s.Define("owner", NewString("jeff"), LayerCLI)
		var diags pulp.Diagnostics
		s.Declare("owner", span, nil, &diags)
		if len(diags) != 0 {
			t.Fatalf("reported %s", diags[0].Plain())
		}
		if got := clean(t, s, "{owner}"); got != "jeff" {
			t.Fatalf("{owner} = %q", got)
		}
		params := s.Params()
		if len(params) != 1 || params[0].From != LayerCLI || params[0].Value != "jeff" {
			t.Fatalf("Params() = %+v", params)
		}
	})

	t.Run("from the environment", func(t *testing.T) {
		env := fakeEnv(map[string]string{"TKB_VAR_OWNER_NAME": "jeff"})
		s := NewScope(date(t, "2026-08-24"), Options{Environ: env})
		var diags pulp.Diagnostics
		s.Declare("owner-name", span, nil, &diags)
		if len(diags) != 0 {
			t.Fatalf("reported %s", diags[0].Plain())
		}
		if got := clean(t, s, "{owner-name}"); got != "jeff" {
			t.Fatalf("{owner-name} = %q, want the TKB_VAR_OWNER_NAME value", got)
		}
		if s.Params()[0].From != LayerEnv {
			t.Fatalf("Params()[0].From = %v, want the environment", s.Params()[0].From)
		}
	})

	t.Run("--var beats the environment", func(t *testing.T) {
		env := fakeEnv(map[string]string{"TKB_VAR_OWNER": "from-env"})
		s := NewScope(date(t, "2026-08-24"), Options{Environ: env})
		s.Define("owner", NewString("from-cli"), LayerCLI)
		var diags pulp.Diagnostics
		s.Declare("owner", span, nil, &diags)
		if got := clean(t, s, "{owner}"); got != "from-cli" {
			t.Fatalf("{owner} = %q", got)
		}
	})

	t.Run("unset is a hard error naming what to pass", func(t *testing.T) {
		s := NewScope(date(t, "2026-08-24"), Options{Environ: fakeEnv(nil)})
		var diags pulp.Diagnostics
		src := pulp.NewSource("t.pulp", "vars\n  owner:\n")
		s.Declare("owner", span, src, &diags)
		if len(diags) != 1 || diags[0].Code != "E211" {
			t.Fatalf("got %v, want E211", diags)
		}
		if !strings.Contains(diags[0].Help, "--var owner=") || !strings.Contains(diags[0].Help, "TKB_VAR_OWNER") {
			t.Fatalf("help %q does not name both ways to supply it", diags[0].Help)
		}
		if diags[0].Span != span {
			t.Fatalf("span %v, want the declaration's %v", diags[0].Span, span)
		}
		if len(s.Params()) != 0 {
			t.Fatalf("an unsatisfied parameter was reported as filled: %+v", s.Params())
		}
	})

	t.Run("declaring a name permits reading it from the environment", func(t *testing.T) {
		env := fakeEnv(map[string]string{"TKB_VAR_HOME": "/planner", "HOME": "/Users/jsc"})
		s := NewScope(date(t, "2026-08-24"), Options{Environ: env})
		var diags pulp.Diagnostics
		s.Declare("HOME", span, nil, &diags)
		if got := clean(t, s, "{env.HOME}"); got != "/Users/jsc" {
			t.Fatalf("{env.HOME} = %q", got)
		}
	})
}

func TestEnvVarName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"owner", "TKB_VAR_OWNER"},
		{"owner-name", "TKB_VAR_OWNER_NAME"},
		{"HOME", "TKB_VAR_HOME"},
	}
	for _, tt := range tests {
		if got := EnvVarName(tt.in); got != tt.want {
			t.Errorf("EnvVarName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestConsumedReportsTheDocumentsInterface(t *testing.T) {
	s := planScope(t)
	clean(t, s, "{owner} in week {week.number}")
	expand(t, s, "{nope}")

	want := []Use{
		{Name: "nope", Resolved: false},
		{Name: "owner", Resolved: true},
		{Name: "week.number", Resolved: true},
	}
	got := s.Consumed()
	if len(got) != len(want) {
		t.Fatalf("Consumed() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Consumed()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestListOnlyIteratesLists(t *testing.T) {
	s := NewScope(date(t, "2026-08-24"), Options{})
	tests := []struct {
		path string
		want int
		ok   bool
	}{
		{"week.days", 7, true},
		{"week.weekdays", 5, true},
		{"week.weekend", 2, true},
		{"month.days", 31, true},
		{"today", 0, false},
		{"week", 0, false},
		{"nope", 0, false},
	}
	for _, tt := range tests {
		items, ok := s.List(tt.path)
		if ok != tt.ok || len(items) != tt.want {
			t.Errorf("List(%q) = %d items, %v; want %d, %v", tt.path, len(items), ok, tt.want, tt.ok)
		}
	}
}

func TestNamesIncludesEveryLayer(t *testing.T) {
	s := NewScope(date(t, "2026-08-24"), Options{})
	s.Define("owner", NewString("jeff"), LayerDocument)
	s.Push()
	s.Bind("day", NewString("Monday"))
	defer s.Pop()

	names := strings.Join(s.Names(), " ")
	for _, want := range []string{"today", "week", "month", "quarter", "year", "doc", "env", "page", "owner", "day"} {
		if !strings.Contains(names, want) {
			t.Errorf("Names() = %q, missing %q", names, want)
		}
	}
}

func TestDocNamespace(t *testing.T) {
	s := NewScope(date(t, "2026-08-24"), Options{Doc: DocInfo{Name: "weekly.pulp", Path: "/tmp/weekly.pulp"}})
	if got := clean(t, s, "{doc.name} at {doc.path}"); got != "weekly.pulp at /tmp/weekly.pulp" {
		t.Fatalf("doc namespace = %q", got)
	}
}
