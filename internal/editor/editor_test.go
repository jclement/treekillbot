package editor

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jclement/treekillbot/internal/pipeline"
)

const goodDocument = `page
  size: a5
section
  height: fill
  panel "Notes"
    height: fill
    line-style: ruled
    line-pitch: 15pt
`

// newTestServer writes a document to a temporary file and returns a server for
// it plus an httptest server carrying its routes.
func newTestServer(t *testing.T, contents string) (*Server, *httptest.Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.pulp")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	anchor := time.Date(2026, time.September, 9, 0, 0, 0, 0, time.UTC)
	server, err := New(Options{Path: path, Build: pipeline.Options{Anchor: anchor, Created: anchor}})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", server.handlePage)
	mux.HandleFunc("/api/render", server.guard(server.handleRender))
	mux.HandleFunc("/api/save", server.guard(server.handleSave))
	listening := httptest.NewServer(mux)
	t.Cleanup(listening.Close)
	return server, listening, path
}

func post(t *testing.T, base, path, token, source string) *http.Response {
	t.Helper()
	body := strings.NewReader(`{"source":` + quote(source) + `}`)
	request, err := http.NewRequest(http.MethodPost, base+path, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("X-Treekillbot-Token", token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func quote(s string) string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}

// The token is not decoration. A page on the open internet can reach 127.0.0.1,
// and without this any site the user has open could read and overwrite the file
// being edited.
func TestRequestsWithoutTheTokenAreRefused(t *testing.T) {
	_, listening, _ := newTestServer(t, goodDocument)

	for _, path := range []string{"/api/render", "/api/save"} {
		response := post(t, listening.URL, path, "", goodDocument)
		if response.StatusCode != 403 {
			t.Errorf("%s without a token = %d, want 403", path, response.StatusCode)
		}
		response.Body.Close()
	}

	response := post(t, listening.URL, "/api/render", "wrong-token", goodDocument)
	if response.StatusCode != 403 {
		t.Errorf("wrong token = %d, want 403", response.StatusCode)
	}
	response.Body.Close()
}

func TestPageRequiresTheToken(t *testing.T) {
	server, listening, _ := newTestServer(t, goodDocument)

	response, err := getURL(listening.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 403 {
		t.Fatalf("page without a token = %d, want 403", response.StatusCode)
	}
	response.Body.Close()

	response, err = getURL(listening.URL + "/?t=" + server.token)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("page with the token = %d, want 200", response.StatusCode)
	}
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), "treekillbot") {
		t.Fatal("the page did not render")
	}
	// The token must never be cached where another process could read it back.
	if cache := response.Header.Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cache)
	}
}

// A cross-origin Origin header is what a DNS-rebinding attack cannot forge.
func TestCrossOriginIsRefused(t *testing.T) {
	server, listening, _ := newTestServer(t, goodDocument)
	request, _ := newRequest(listening.URL+"/api/render", goodDocument)
	request.Header.Set("X-Treekillbot-Token", server.token)
	request.Header.Set("Origin", "https://evil.example.com")

	response, err := doRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatalf("cross-origin request = %d, want 403", response.StatusCode)
	}
}

func TestRenderReturnsSVGAndGeometry(t *testing.T) {
	server, listening, _ := newTestServer(t, goodDocument)
	response := post(t, listening.URL, "/api/render", server.token, goodDocument)
	defer response.Body.Close()

	var payload renderResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK {
		t.Fatalf("expected a clean render, got %+v", payload.Diagnostics)
	}
	if !strings.HasPrefix(payload.SVG, "<svg") {
		t.Fatalf("SVG = %.60s", payload.SVG)
	}
	// A5 is 148x210mm, which is 419.5x595.25pt.
	if payload.Width < 419 || payload.Width > 420 {
		t.Fatalf("width = %g, want about 419.5", payload.Width)
	}
	if payload.Height < 595 || payload.Height > 596 {
		t.Fatalf("height = %g, want about 595.25", payload.Height)
	}
}

// While someone is mid-keystroke the document is usually broken. Blanking the
// preview every time would make the editor flicker unusably, so a document with
// errors still renders whatever geometry survived.
func TestBrokenDocumentStillReportsAndStillPreviews(t *testing.T) {
	server, listening, _ := newTestServer(t, goodDocument)
	broken := "section\n  panel \"A\"\n    height: 200\n    line-stile: ruled\n"

	response := post(t, listening.URL, "/api/render", server.token, broken)
	defer response.Body.Close()

	var payload renderResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.OK {
		t.Fatal("a document with errors must not report ok")
	}
	if len(payload.Diagnostics) < 2 {
		t.Fatalf("expected both errors, got %d", len(payload.Diagnostics))
	}
	for _, d := range payload.Diagnostics {
		if d.Line == 0 || d.Column == 0 {
			t.Errorf("diagnostic %s has no position, so the editor cannot mark it", d.Code)
		}
	}
}

func TestSaveWritesTheFileAndPreservesItsMode(t *testing.T) {
	server, listening, path := newTestServer(t, goodDocument)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	edited := goodDocument + "\nsection\n  text \"added\"\n"
	response := post(t, listening.URL, "/api/save", server.token, edited)
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("save = %d", response.StatusCode)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != edited {
		t.Fatal("the file does not match what was saved")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Forcing 0644 would quietly widen the permissions of a document someone
	// deliberately made private.
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600 preserved", info.Mode().Perm())
	}
}

func TestSaveOnlyEverTouchesItsOwnFile(t *testing.T) {
	server, listening, path := newTestServer(t, goodDocument)
	neighbour := filepath.Join(filepath.Dir(path), "other.pulp")
	if err := os.WriteFile(neighbour, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}

	response := post(t, listening.URL, "/api/save", server.token, "changed")
	response.Body.Close()

	// The path is fixed at construction and never comes from the request, so
	// there is no traversal to attempt — this test pins that property.
	if data, _ := os.ReadFile(neighbour); string(data) != "untouched" {
		t.Fatal("a save reached a file other than the one being edited")
	}
}

func TestGetIsRefusedOnPostEndpoints(t *testing.T) {
	server, listening, _ := newTestServer(t, goodDocument)
	response, err := getURL(listening.URL + "/api/save?t=" + server.token)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatalf("GET on save = %d, want 400", response.StatusCode)
	}
}

func getURL(url string) (*http.Response, error) { return http.Get(url) }

func newRequest(url, source string) (*http.Request, error) {
	return http.NewRequest(http.MethodPost, url, strings.NewReader(`{"source":`+quote(source)+`}`))
}

func doRequest(r *http.Request) (*http.Response, error) { return http.DefaultClient.Do(r) }
