// Update checking: ask GitHub whether a newer release exists. Downloading and
// installing one is deliberately not here yet — that path has to verify a
// sigstore bundle before it writes anything, and half of it is worse than none.

package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultAPIURL is GitHub's "latest release" endpoint. It excludes
	// drafts and prereleases for us, so nothing here has to filter them.
	defaultAPIURL = "https://api.github.com/repos/jclement/treekillbot/releases/latest"

	// defaultTimeout bounds the whole check. A version banner is never worth
	// making the user wait, so this is short and the caller is expected to
	// run the check off the critical path.
	defaultTimeout = 10 * time.Second
)

// ErrSkipped is returned when the check did not run at all, as distinct from
// running and finding nothing. Callers that surface an update notice want to
// stay silent for both, but anything reporting *why* needs to tell them apart.
var ErrSkipped = errors.New("update check skipped")

// Result is what one completed check found.
type Result struct {
	Current         string    // the running version, without a "v"
	Latest          string    // the newest released version, without a "v"
	TagName         string    // the release's git tag, e.g. "v1.2.3"
	ReleaseURL      string    // the GitHub release page
	UpdateAvailable bool      // Latest is strictly newer than Current
	CheckedAt       time.Time // from Checker.Now, so callers can cache this
}

// Checker queries GitHub for the latest release.
//
// Every external dependency is a field so the whole thing is testable without
// a network, a container, or a wall clock: the zero value works in production
// and a test supplies its own httptest server, clock and container predicate.
type Checker struct {
	// HTTPClient issues the request. Defaults to a client whose timeout is
	// defaultTimeout.
	HTTPClient *http.Client

	// Now supplies Result.CheckedAt. Defaults to time.Now.
	Now func() time.Time

	// APIURL is the releases endpoint. Defaults to defaultAPIURL.
	APIURL string

	// Current is the version to compare against. Defaults to Version, the
	// linker-stamped one.
	Current string

	// InContainer reports whether we are running inside a container image,
	// where replacing the binary is the wrong answer and the check is
	// therefore pointless. Defaults to detecting Docker and Podman.
	InContainer func() bool
}

// release is the slice of GitHub's release JSON we actually read.
type release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// Check asks GitHub for the latest release and compares it to the running
// version.
//
// It returns ErrSkipped when there is nothing sensible to check — an unstamped
// "dev" build, or a container, where the fix is to pull a new image rather
// than to rewrite the binary under a running process. Any other error means
// the check was attempted and failed; callers should treat that as "unknown"
// and stay quiet rather than surfacing it, since being offline is normal.
func (c *Checker) Check(ctx context.Context) (*Result, error) {
	current := strings.TrimPrefix(c.current(), "v")
	if current == "" || current == "dev" {
		return nil, fmt.Errorf("%w: unstamped development build", ErrSkipped)
	}
	if c.inContainer() {
		return nil, fmt.Errorf("%w: running in a container", ErrSkipped)
	}

	rel, err := c.fetchLatest(ctx)
	if err != nil {
		return nil, err
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	return &Result{
		Current:         current,
		Latest:          latest,
		TagName:         rel.TagName,
		ReleaseURL:      rel.HTMLURL,
		UpdateAvailable: CompareVersions(latest, current) > 0,
		CheckedAt:       c.now(),
	}, nil
}

// fetchLatest performs the HTTP request and decodes the release.
func (c *Checker) fetchLatest(ctx context.Context) (*release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL(), nil)
	if err != nil {
		return nil, fmt.Errorf("building update request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", Name+"/"+Version)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying %s: %w", c.apiURL(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("querying %s: %s", c.apiURL(), resp.Status)
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decoding release from %s: %w", c.apiURL(), err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("release from %s has no tag_name", c.apiURL())
	}
	return &rel, nil
}

// ---- Defaults ----

func (c *Checker) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: defaultTimeout}
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) apiURL() string {
	if c.APIURL != "" {
		return c.APIURL
	}
	return defaultAPIURL
}

func (c *Checker) current() string {
	if c.Current != "" {
		return c.Current
	}
	return Version
}

func (c *Checker) inContainer() bool {
	if c.InContainer != nil {
		return c.InContainer()
	}
	return runningInContainer()
}

// runningInContainer detects Docker and Podman. Docker's daemon creates
// /.dockerenv in every container it starts; Podman and most OCI runtimes set
// container=<engine> instead. Neither is guaranteed by any spec, but a false
// negative here only costs a pointless HTTP request, so cheap beats thorough.
func runningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	return os.Getenv("container") != ""
}

// ---- Semver comparison ----

// CompareVersions returns -1, 0 or +1 as a sorts before, equal to, or after b.
//
// This handles the subset of semver our tags actually use: three numeric
// components with an optional prerelease or build suffix. Per the spec a
// prerelease sorts *before* the release it precedes, so 1.2.0-rc1 < 1.2.0 —
// which is the case that matters, since it is what stops an rc build from
// being told it is already up to date. Prerelease identifiers are not ordered
// against each other; rc1 and rc2 compare equal. A version that does not parse
// sorts before one that does, so garbage never claims to be an upgrade.
func CompareVersions(a, b string) int {
	numsA, preA, okA := parseVersion(a)
	numsB, preB, okB := parseVersion(b)

	switch {
	case !okA && !okB:
		return 0
	case !okA:
		return -1
	case !okB:
		return 1
	}

	for i := range numsA {
		if numsA[i] != numsB[i] {
			if numsA[i] < numsB[i] {
				return -1
			}
			return 1
		}
	}

	switch {
	case preA == preB:
		return 0
	case preA:
		return -1
	default:
		return 1
	}
}

// parseVersion splits "1.2.3-rc1" into its numeric components and whether a
// prerelease suffix was present. ok is false if it is not three numbers.
func parseVersion(s string) (nums [3]int, prerelease bool, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")

	// Build metadata ("+abc") is explicitly not part of precedence, so it is
	// dropped before the prerelease suffix is even looked for.
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s, prerelease = s[:i], true
	}

	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return nums, prerelease, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nums, prerelease, false
		}
		nums[i] = n
	}
	return nums, prerelease, true
}
