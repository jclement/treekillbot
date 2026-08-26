package version

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTag(t *testing.T) {
	tests := []struct {
		version string
		want    string
	}{
		// GoReleaser stamps "1.2.3"; every human surface says "v1.2.3".
		{"1.2.3", "v1.2.3"},
		{"0.0.1", "v0.0.1"},
		{"1.2.0-rc1", "v1.2.0-rc1"},
		// Unstamped and `git describe` builds pass through untouched.
		{"dev", "dev"},
		{"", "dev"},
	}
	for _, tc := range tests {
		t.Run(tc.version, func(t *testing.T) {
			withVersion(t, tc.version)
			if got := Tag(); got != tc.want {
				t.Errorf("Tag() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The one-line form is parsed by humans reading bug reports and by nothing
// else, but it is specified, so it is pinned.
func TestStringFormat(t *testing.T) {
	withVersion(t, "1.2.3")
	Commit, BuildDate = "abc1234", "2026-01-15T10:30:00Z"

	const want = "treekillbot v1.2.3 (abc1234, 2026-01-15T10:30:00Z)"
	if got := String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	detailed := Detailed()
	if !strings.HasPrefix(detailed, want) {
		t.Errorf("Detailed() = %q, want it to start with String()", detailed)
	}
	for _, want := range []string{"go:", "platform:"} {
		if !strings.Contains(detailed, want) {
			t.Errorf("Detailed() = %q, want it to mention %q", detailed, want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.4", "1.2.3", 1},
		{"1.3.0", "1.2.99", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.2.3", "1.2.4", -1},
		// Leading "v" is noise on either side.
		{"v1.2.3", "1.2.3", 0},
		// The case that stops an rc telling itself it is current.
		{"1.2.0", "1.2.0-rc1", 1},
		{"1.2.0-rc1", "1.2.0", -1},
		{"1.2.0-rc1", "1.2.0-rc2", 0},
		// Build metadata takes no part in precedence.
		{"1.2.3+deadbeef", "1.2.3", 0},
		// Ten is not two, however it sorts as a string.
		{"1.10.0", "1.9.0", 1},
		// Unparseable never claims to be an upgrade.
		{"dev", "1.2.3", -1},
		{"1.2.3", "dev", 1},
		{"", "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.a+"_vs_"+tc.b, func(t *testing.T) {
			if got := CompareVersions(tc.a, tc.b); got != tc.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// A check that runs is a check that made an HTTP request; these are the two
// cases where it must not make one at all.
func TestCheckSkips(t *testing.T) {
	tests := []struct {
		name        string
		current     string
		inContainer bool
	}{
		{"dev build", "dev", false},
		{"container", "1.0.0", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checker := &Checker{
				Current:     tc.current,
				InContainer: func() bool { return tc.inContainer },
				APIURL:      mustNotBeCalled(t),
			}

			result, err := checker.Check(t.Context())
			if result != nil {
				t.Errorf("Check() returned %+v, want nil", result)
			}
			if err == nil || !isSkipped(err) {
				t.Fatalf("Check() error = %v, want ErrSkipped", err)
			}
		})
	}
}

func TestCheckFindsUpdate(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		current string
		want    bool
	}{
		{"newer release", "v1.3.0", "1.2.3", true},
		{"same release", "v1.2.3", "1.2.3", false},
		// Downgrades happen: someone installs a build from a branch, or a
		// release is yanked. Neither is an "update available".
		{"older release", "v1.2.0", "1.2.3", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := releaseServer(t, `{"tag_name":"`+tc.tag+`","html_url":"https://example.test/rel"}`)
			frozen := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)

			checker := &Checker{
				APIURL:      server.URL,
				HTTPClient:  server.Client(),
				Current:     tc.current,
				InContainer: func() bool { return false },
				Now:         func() time.Time { return frozen },
			}

			result, err := checker.Check(t.Context())
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if result.UpdateAvailable != tc.want {
				t.Errorf("UpdateAvailable = %v, want %v", result.UpdateAvailable, tc.want)
			}
			if result.TagName != tc.tag {
				t.Errorf("TagName = %q, want %q", result.TagName, tc.tag)
			}
			if result.Latest != strings.TrimPrefix(tc.tag, "v") {
				t.Errorf("Latest = %q, want %q", result.Latest, strings.TrimPrefix(tc.tag, "v"))
			}
			if !result.CheckedAt.Equal(frozen) {
				t.Errorf("CheckedAt = %v, want the injected clock's %v", result.CheckedAt, frozen)
			}
		})
	}
}

// Being offline is the normal case, not an exceptional one, so the failure
// modes have to be errors rather than panics or zero-valued Results.
func TestCheckTransportAndBodyFailures(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"rate limited", http.StatusForbidden, `{}`, "403"},
		{"not found", http.StatusNotFound, `{}`, "404"},
		{"malformed json", http.StatusOK, `{"tag_name":`, "decoding release"},
		{"no tag in response", http.StatusOK, `{"html_url":"x"}`, "no tag_name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(server.Close)

			checker := &Checker{
				APIURL:      server.URL,
				HTTPClient:  server.Client(),
				Current:     "1.2.3",
				InContainer: func() bool { return false },
			}

			result, err := checker.Check(t.Context())
			if result != nil {
				t.Errorf("Check() returned %+v, want nil", result)
			}
			if err == nil {
				t.Fatal("Check() error = nil, want a failure")
			}
			if isSkipped(err) {
				t.Errorf("Check() error = %v, want a real failure not ErrSkipped", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Check() error = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

// The caller's context has to be able to abandon the check; a version banner
// must never be the reason a command hangs.
func TestCheckHonoursContext(t *testing.T) {
	server := releaseServer(t, `{"tag_name":"v9.9.9"}`)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	checker := &Checker{
		APIURL:      server.URL,
		HTTPClient:  server.Client(),
		Current:     "1.2.3",
		InContainer: func() bool { return false },
	}
	if _, err := checker.Check(ctx); err == nil {
		t.Fatal("Check() with a cancelled context returned nil error")
	}
}

// ---- Helpers ----

// withVersion sets the linker-stamped variables for one test and restores
// them afterwards, so package-level state does not leak between subtests.
func withVersion(t *testing.T, v string) {
	t.Helper()
	oldVersion, oldCommit, oldDate := Version, Commit, BuildDate
	t.Cleanup(func() { Version, Commit, BuildDate = oldVersion, oldCommit, oldDate })
	Version = v
}

// releaseServer serves body once, and fails the test if the request does not
// look like the one we mean to send.
func releaseServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept header = %q, want the GitHub media type", got)
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), Name+"/") {
			t.Errorf("User-Agent = %q, want it to start with %q", r.Header.Get("User-Agent"), Name+"/")
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// mustNotBeCalled returns a URL that fails the test if it is ever fetched.
func mustNotBeCalled(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("update check made an HTTP request when it should have skipped")
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func isSkipped(err error) bool {
	return errors.Is(err, ErrSkipped)
}
