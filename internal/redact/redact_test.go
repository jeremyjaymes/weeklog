package redact

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

func loadSamples(t *testing.T) []Sample {
	t.Helper()
	samples, err := LoadSamples(filepath.Join("..", "..", "testdata", "secrets", "corpus.json"))
	if err != nil {
		t.Fatalf("LoadSamples: %v", err)
	}
	if len(samples) < 15 {
		t.Fatalf("corpus has %d samples", len(samples))
	}
	return samples
}

func configSecrets(samples []Sample) []string {
	var out []string
	for _, sample := range samples {
		if sample.NeedsKey {
			out = append(out, sample.Secret)
		}
	}
	return out
}

func keyedRedactor(t *testing.T, samples []Sample) *Redactor {
	t.Helper()
	r, err := New(Options{Secrets: configSecrets(samples)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func hide(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "[canary]")
}

func TestCorpusLabels(t *testing.T) {
	samples := loadSamples(t)
	def := Default()
	keyed := keyedRedactor(t, samples)
	if len(configSecrets(samples)) == 0 {
		t.Fatal("corpus has no config secret")
	}

	for _, sample := range samples {
		sample := sample
		t.Run(sample.Name, func(t *testing.T) {
			plain := def.Text(sample.Text)
			if sample.NeedsKey {
				if !strings.Contains(plain, sample.Secret) {
					t.Fatalf("config secret was removed without being configured: %s", hide(plain, sample.Secret))
				}
			}
			r := def
			if sample.NeedsKey {
				r = keyed
			}
			got := r.Text(sample.Text)
			want := Marker(sample.Label)
			if !strings.Contains(got, want) {
				t.Fatalf("redacted to %q, want %s", hide(got, sample.Secret), want)
			}
			if strings.Contains(got, sample.Secret) {
				t.Fatalf("secret remains in %s", hide(got, sample.Secret))
			}
			for _, ban := range sample.AlsoBan {
				if strings.Contains(got, ban) {
					t.Fatalf("banned substring remains in %s", hide(got, ban))
				}
			}
			for _, keep := range sample.Keep {
				if !strings.Contains(got, keep) {
					t.Fatalf("lost %q in %s", keep, hide(got, sample.Secret))
				}
			}
			wrapped := r.Text("note " + sample.Text + " end")
			if strings.Contains(wrapped, sample.Secret) || !strings.Contains(wrapped, want) {
				t.Fatalf("wrapped redaction = %s", hide(wrapped, sample.Secret))
			}
			if again := r.Text(got); again != got {
				t.Fatalf("not idempotent:\n%q\n%q", hide(got, sample.Secret), hide(again, sample.Secret))
			}
		})
	}
}

func TestTokenVariants(t *testing.T) {
	r := Default()
	tests := []struct {
		name  string
		text  string
		label string
	}{
		{name: "gho", text: "gho_" + strings.Repeat("a", 36), label: labelGitHub},
		{name: "ghu", text: "ghu_" + strings.Repeat("a", 36), label: labelGitHub},
		{name: "ghs", text: "ghs_" + strings.Repeat("a", 36), label: labelGitHub},
		{name: "ghr", text: "ghr_" + strings.Repeat("a", 36), label: labelGitHub},
		{name: "xoxp", text: "xoxp-" + strings.Repeat("a", 20), label: labelSlack},
		{name: "xoxa", text: "xoxa-" + strings.Repeat("b", 20), label: labelSlack},
		{name: "xoxr", text: "xoxr-" + strings.Repeat("c", 20), label: labelSlack},
		{name: "xoxs", text: "xoxs-" + strings.Repeat("d", 20), label: labelSlack},
		{name: "rk_live", text: "rk_live_" + strings.Repeat("e", 24), label: labelStripe},
		{name: "sk_test", text: "sk_test_" + strings.Repeat("f", 24), label: labelStripe},
		{name: "sk-proj", text: "sk-proj-" + strings.Repeat("h", 32), label: labelOpenAI},
		{name: "aws-asia", text: "ASIA" + strings.Repeat("B", 16), label: labelAWSAccess},
		{name: "quoted-aws-secret", text: `aws_secret_access_key="` + strings.Repeat("Aa1+", 10) + `"`, label: labelAWSSecret},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Text(tt.text)
			if !strings.Contains(got, Marker(tt.label)) {
				t.Fatalf("got %s", hide(got, tt.text))
			}
			secret := tt.text
			if tt.name == "quoted-aws-secret" {
				secret = strings.Repeat("Aa1+", 10)
			}
			if strings.Contains(got, secret) {
				t.Fatalf("secret remains in %s", hide(got, secret))
			}
		})
	}
}

func TestAssignmentsAndKeys(t *testing.T) {
	r := Default()
	cases := []struct {
		name  string
		text  string
		label string
		gone  string
		keep  string
	}{
		{name: "equals", text: "password=hunter2weeklog", label: labelPassword, gone: "hunter2weeklog", keep: "password="},
		{name: "equals spaced", text: "api_token = wkspacedtoken", label: labelToken, gone: "wkspacedtoken", keep: "api_token="},
		{name: "double quotes", text: `passwd="quoted-secret-value"`, label: labelPassword, gone: "quoted-secret-value"},
		{name: "single quotes", text: "secret='single-quote-value'", label: labelSecret, gone: "single-quote-value"},
		{name: "api-key", text: "api-key=wk-api-key-value", label: labelAPIKey, gone: "wk-api-key-value"},
		{name: "apikey", text: "apikey=wkapikeyvalue1", label: labelAPIKey, gone: "wkapikeyvalue1"},
		{name: "private", text: "private_key=wkprivatevalue1", label: labelPrivate, gone: "wkprivatevalue1"},
		{name: "json", text: `"api_key": "wkjsonapikeyvalue"`, label: labelAPIKey, gone: "wkjsonapikeyvalue", keep: `"api_key"`},
		{name: "yaml", text: "client_secret: wkyamlsecretvalue", label: labelSecret, gone: "wkyamlsecretvalue"},
		{name: "url keeps host", text: "https://wkuser:wkpass-9f3a2c1b@example.com/repo.git", label: labelURL, gone: "wkpass-9f3a2c1b", keep: "example.com/repo.git"},
		{name: "bearer", text: "Bearer wkBearerTokenValue123", label: labelBearer, gone: "wkBearerTokenValue123"},
		{name: "pem openssh", text: "-----BEGIN OPENSSH PRIVATE KEY-----\nWEEKLOGFAKEOPENSSHKEY\n-----END OPENSSH PRIVATE KEY-----", label: labelPrivateKey, gone: "WEEKLOGFAKEOPENSSHKEY"},
		{name: "pem pgp", text: "-----BEGIN PGP PRIVATE KEY BLOCK-----\nWEEKLOGFAKEPGPKEY\n-----END PGP PRIVATE KEY BLOCK-----", label: labelPrivateKey, gone: "WEEKLOGFAKEPGPKEY"},
		{name: "ordinary name", text: "name=alice", label: "", gone: "", keep: "name=alice"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Text(tt.text)
			if tt.label == "" {
				if got != tt.text {
					t.Fatalf("changed to %q", got)
				}
				return
			}
			if !strings.Contains(got, Marker(tt.label)) || strings.Contains(got, tt.gone) {
				t.Fatalf("got %s", hide(got, tt.gone))
			}
			if tt.keep != "" && !strings.Contains(got, tt.keep) {
				t.Fatalf("lost %q in %q", tt.keep, hide(got, tt.gone))
			}
		})
	}
}

func TestAllowExtraAndConfigSecrets(t *testing.T) {
	token := "ghp_" + strings.Repeat("z", 36)
	custom := "WKCUST-654321"
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	other := "ZYXWVUTSRQPONMLKJIHGFEDCBA"
	cfg := "p+ss.w0rd!"
	short := "s3cret"
	inline := "s3cret99"

	r, err := New(Options{
		ExtraPatterns: []string{`WKCUST-[0-9]{6}`},
		Allow:         []string{token, alphabet},
		Secrets:       []string{cfg, short, inline, "  " + cfg + "  ", cfg},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := r.Text("see " + token + " please"); got != "see "+token+" please" {
		t.Fatalf("allowed token changed: %s", hide(got, token))
	}
	if got := r.Text(alphabet); strings.Contains(got, "[REDACTED:") {
		t.Fatalf("allowed alphabet redacted: %s", got)
	}
	if got := r.Text("id " + other); strings.Contains(got, other) || !strings.Contains(got, Marker(labelHighEntropy)) {
		t.Fatalf("other entropy = %s", got)
	}
	if got := r.Text("id " + custom + "."); strings.Contains(got, custom) || !strings.Contains(got, Marker(labelCustom)) {
		t.Fatalf("custom = %s", got)
	}
	if got := r.Text("see " + cfg + " now"); strings.Contains(got, cfg) || !strings.Contains(got, Marker(labelConfig)) {
		t.Fatalf("config = %s", hide(got, cfg))
	}
	if got := r.Text("use " + short + " now"); strings.Contains(got, short) {
		t.Fatalf("short secret kept: %s", got)
	}
	if got := r.Text("x" + short + "y"); got != "x"+short+"y" {
		t.Fatalf("short secret ate a word: %q", got)
	}
	if got := r.Text("x" + inline + "y"); strings.Contains(got, inline) {
		t.Fatalf("inline secret kept: %s", got)
	}

	long := "weeklog-cfg-9f3a2c1b-extra"
	shortCfg := "weeklog-cfg-9f3a2c1b"
	longer, err := New(Options{Secrets: []string{shortCfg, long}})
	if err != nil {
		t.Fatal(err)
	}
	if got := longer.Text("see " + long); strings.Contains(got, "extra") || strings.Contains(got, shortCfg) {
		t.Fatalf("longest secret was split: %s", hide(got, long))
	}
}

func TestNewRejectsBadOptions(t *testing.T) {
	if _, err := New(Options{ExtraPatterns: []string{""}}); err == nil {
		t.Fatal("empty pattern")
	}
	if _, err := New(Options{ExtraPatterns: []string{"("}}); err == nil {
		t.Fatal("bad pattern")
	}
	if _, err := New(Options{Allow: []string{""}}); err == nil {
		t.Fatal("empty allow")
	}
	if _, err := New(Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestSafeStringsStay(t *testing.T) {
	r := Default()
	samples := []string{
		"",
		"Fix the webhook retry bug",
		"https://github.com/example/acme-app/pull/12",
		"http://localhost:8080/health",
		"github.com/example/acme-app",
		"cursor/fix-webhooks",
		"/Users/me/Projects/acme-app/app/Webhooks.php",
		"GEN-301",
		"this is not a secret",
		"0123456789abcdef0123456789abcdef01234567",
		strings.Repeat("0123456789abcdef", 4),
		"123e4567-e89b-12d3-a456-426614174000",
		"aaaaaaaaaaaaaaaaaaaaaaaa",
		"café " + "resume",
		"tokens_in",
	}
	for _, sample := range samples {
		if got := r.Text(sample); got != sample {
			t.Fatalf("changed %q to %q", sample, got)
		}
	}
}

func TestPlanExampleUntouched(t *testing.T) {
	end := time.Date(2026, 10, 8, 15, 40, 0, 0, time.UTC)
	in := event.Event{
		Source:   "claude-code",
		SourceID: "sess_abc#3",
		Kind:     event.KindAgentSession,
		TS:       time.Date(2026, 10, 8, 14, 2, 11, 0, time.UTC),
		TSEnd:    &end,
		Actor:    "jeremyjaymes",
		Title:    "Fix Stripe webhook retries",
		Summary:  "first user prompt or the commit subject, truncated to 500 chars",
		URL:      "https://github.com/example/acme-app/pull/12",
		Repo:     "github.com/example/acme-app",
		Branch:   "cursor/fix-webhooks",
		CWD:      "/Users/me/Projects/acme-app",
		Files:    []string{"app/Webhooks.php"},
		Refs:     []string{"GEN-301", "#12", "a1b2c3d"},
		State:    event.StateMerged,
		Metrics: map[string]float64{
			"tokens_in":   1200,
			"tokens_out":  800,
			"lines_added": 40,
		},
		BodyRef: "file:///tmp/sess.jsonl",
	}
	got := Default().Event(in)
	if got.Title != in.Title || got.Summary != in.Summary || got.URL != in.URL || got.State != in.State {
		t.Fatalf("plan example changed: %#v", got)
	}
	if len(got.Files) != 1 || got.Files[0] != in.Files[0] || &got.Files[0] == &in.Files[0] {
		t.Fatalf("files = %#v", got.Files)
	}
}

func TestEventRedactsFieldsWithoutMutatingInput(t *testing.T) {
	token := "ghp_" + strings.Repeat("q", 36)
	end := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	in := event.Event{
		Source:   "git " + token,
		SourceID: "id/" + token,
		Kind:     event.Kind("commit " + token),
		TSEnd:    &end,
		Actor:    token,
		Title:    "title " + token,
		Summary:  token,
		URL:      "https://user:" + token + "@example.com/a",
		Repo:     token,
		Branch:   token,
		CWD:      token,
		Files:    []string{token, "keep.go"},
		Refs:     []string{token},
		State:    event.State("merged " + token),
		Metrics: map[string]float64{
			token:                            1,
			"tokens_in":                      2,
			"ghp_" + strings.Repeat("r", 36): 3,
		},
		BodyRef: "file://" + token,
	}
	filesBefore := append([]string(nil), in.Files...)
	got := Default().Event(in)

	if in.Title != "title "+token || in.Files[0] != filesBefore[0] {
		t.Fatal("input was mutated")
	}
	if got.TSEnd == nil || !got.TSEnd.Equal(end) {
		t.Fatal("lost ts_end")
	}
	end = end.Add(time.Hour)
	if got.TSEnd.Equal(end) {
		t.Fatal("ts_end aliased")
	}
	if got.Kind != event.KindCommit {
		t.Fatalf("kind = %q", got.Kind)
	}
	if got.State != event.StateMerged {
		t.Fatalf("state = %q", got.State)
	}
	blob := fmt.Sprintf("%#v", got)
	if strings.Contains(blob, token) || strings.Contains(blob, strings.Repeat("r", 36)) {
		t.Fatalf("event still has a secret: %s", hide(blob, token))
	}
	if got.Metrics["tokens_in"] != 2 {
		t.Fatalf("metrics = %#v", got.Metrics)
	}
	if _, ok := got.Metrics[Marker(labelGitHub)]; !ok || len(got.Metrics) != 2 {
		t.Fatalf("metrics = %#v", got.Metrics)
	}
	if got.Files[1] != "keep.go" {
		t.Fatalf("files = %#v", got.Files)
	}
	if in.Metrics[token] != 1 {
		t.Fatal("input metrics changed")
	}
}

func TestStateAndKindOnlySecret(t *testing.T) {
	token := "ghp_" + strings.Repeat("m", 36)
	got := Default().Event(event.Event{
		Kind:  event.Kind(token),
		State: event.State(token),
	})
	if got.Kind != event.Kind(Marker(labelGitHub)) {
		t.Fatalf("kind = %q", got.Kind)
	}
	if got.State != "" {
		t.Fatalf("state = %q", got.State)
	}
}

func TestNilAndZeroRedactor(t *testing.T) {
	token := "ghp_" + strings.Repeat("n", 36)
	var none *Redactor
	if strings.Contains(none.Text(token), token) {
		t.Fatal("nil redactor kept the token")
	}
	var zero Redactor
	if strings.Contains(zero.Text(token), token) {
		t.Fatal("zero redactor kept the token")
	}
}

func TestRedactorFormatHidesSecrets(t *testing.T) {
	secret := "weeklog-cfg-9f3a2c1b"
	r, err := New(Options{Secrets: []string{secret}, Allow: []string{"keep-me"}, ExtraPatterns: []string{`WKCUST-[0-9]{4}`}})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{r.String(), fmt.Sprintf("%v", r), fmt.Sprintf("%#v", r)} {
		if strings.Contains(text, secret) || strings.Contains(text, "keep-me") || strings.Contains(text, "WKCUST") {
			t.Fatalf("format leaked config: %s", hide(text, secret))
		}
	}
}

func TestTextParallel(t *testing.T) {
	r := Default()
	token := "ghp_" + strings.Repeat("p", 36)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if strings.Contains(r.Text(token), token) {
				t.Error("parallel redact kept the token")
			}
		}()
	}
	wg.Wait()
}

func TestRenderedMarkdownHasNoSecrets(t *testing.T) {
	samples := loadSamples(t)
	r := keyedRedactor(t, samples)
	var doc strings.Builder
	doc.WriteString("# Week 2026-W41\n\n")
	for _, sample := range samples {
		fmt.Fprintf(&doc, "### %s\n%s\n\n", sample.Name, sample.Text)
	}
	got := r.Text(doc.String())
	if !strings.Contains(got, "# Week 2026-W41") {
		t.Fatal("heading was removed")
	}
	for _, sample := range samples {
		if strings.Contains(got, sample.Secret) {
			t.Fatalf("%s leaked into markdown: %s", sample.Name, hide(got, sample.Secret))
		}
		for _, ban := range sample.AlsoBan {
			if strings.Contains(got, ban) {
				t.Fatalf("%s banned text leaked", sample.Name)
			}
		}
	}
}

func TestShannonAndHex(t *testing.T) {
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	if shannon(alphabet) < entropyThreshold {
		t.Fatalf("alphabet entropy %v", shannon(alphabet))
	}
	sha := "0123456789abcdef0123456789abcdef01234567"
	if !isHexLike(sha) {
		t.Fatal("sha not hex-like")
	}
	if shannon(strings.Repeat("a", 24)) >= entropyThreshold {
		t.Fatal("repeated char looked random")
	}
	if isHexLike(alphabet) {
		t.Fatal("alphabet classified as hex")
	}
}

func TestLoadSamplesRejects(t *testing.T) {
	if _, err := LoadSamples(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing file")
	}
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cases := []string{
		`{"version":2,"samples":[{"name":"a","label":"jwt","build":"jwt"}]}`,
		`{"version":1,"samples":[]}`,
		`{"version":1,"samples":[{"name":"a","label":"jwt","build":"nope"}]}`,
		`{"version":1,"samples":[{"name":"a","label":"jwt","build":"jwt"},{"name":"a","label":"jwt","build":"jwt"}]}`,
		`{"version":1,"samples":[{"name":"","label":"jwt","build":"jwt"}]}`,
		`{"version":1,"samples":[{"name":"a","label":"NOT A LABEL","build":"jwt"}]}`,
		`{"version":1,"extra":true,"samples":[]}`,
		`{"version":1,"samples":[{"name":"a","label":"jwt","build":"jwt"}]}{}`,
	}
	for i, body := range cases {
		if _, err := LoadSamples(write(fmt.Sprintf("%d.json", i), body)); err == nil {
			t.Fatalf("case %d accepted %s", i, body)
		}
	}
}

func TestMarkerSanitizesKind(t *testing.T) {
	if Marker("GitHub Token") != "[REDACTED:custom]" {
		t.Fatal(Marker("GitHub Token"))
	}
	if Marker("github-token") != "[REDACTED:github-token]" {
		t.Fatal(Marker("github-token"))
	}
	if n, ok := markerLen("[REDACTED:"); ok || n != 0 {
		t.Fatalf("open marker %d %v", n, ok)
	}
	if n, ok := markerLen("[REDACTED:NOT GOOD]"); ok || n != 0 {
		t.Fatalf("bad marker %d %v", n, ok)
	}
	want := len("[REDACTED:github-token]")
	if n, ok := markerLen("[REDACTED:github-token]tail"); !ok || n != want {
		t.Fatalf("marker len %d %v", n, ok)
	}
}

func TestNilRedactorAndEmptyEdges(t *testing.T) {
	var none *Redactor
	if none.String() != "Redactor(nil)" {
		t.Fatal(none.String())
	}
	token := "ghp_" + strings.Repeat("v", 36)
	got := none.Event(event.Event{Title: token, Metrics: map[string]float64{"": 1}})
	if strings.Contains(got.Title, token) {
		t.Fatal("nil Event kept the token")
	}
	if _, ok := got.Metrics[Marker(labelHighEntropy)]; !ok {
		t.Fatalf("empty metric key = %#v", got.Metrics)
	}
	if shannon("") != 0 || isHexLike("") || !isHexLike("abc") {
		t.Fatal("entropy helpers")
	}
	if labelForKey("nothing") != labelSecret {
		t.Fatal(labelForKey("nothing"))
	}
	if firstNonEmpty("", "") != "" {
		t.Fatal("firstNonEmpty")
	}
	if redactState("custom") != "custom" || redactState("") != "" || redactState(" merged ") != event.StateMerged {
		t.Fatalf("state %q %q", redactState("custom"), redactState(" merged "))
	}
	if !overlaps([][2]int{{1, 4}}, 3, 6) || overlaps(nil, 0, 1) {
		t.Fatal("overlaps")
	}
	allow := map[string]struct{}{"keep": {}}
	if !allowed(allow, "other", []string{"", "keep"}) || allowed(map[string]struct{}{}, "keep", nil) {
		t.Fatal("allowed")
	}
}

func TestSecretBoundariesAndSkippedValues(t *testing.T) {
	short := "s3cret"
	r, err := New(Options{
		Allow:   []string{"abcd1234"},
		Secrets: []string{"   ", "abcd1234", "abcd1235", "abcd1235", "zzzz1235", short},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.String(); !strings.Contains(got, "secrets=3") {
		t.Fatal(got)
	}
	if got := r.Text("s3crety"); got != "s3crety" {
		t.Fatalf("right boundary: %q", got)
	}
	if strings.Contains(r.Text("(s3cret)"), short) || strings.Contains(r.Text("use s3cret"), short) {
		t.Fatal("bounded short secret kept")
	}
	if got := r.Text(`password=""`); got != `password=""` {
		t.Fatalf("empty password changed: %q", got)
	}
	if got := r.Text(`"password": ""`); got != `"password": ""` {
		t.Fatalf("empty json changed: %q", got)
	}
	cfg := "wkpass-9f3a2c1b"
	withURL, err := New(Options{Secrets: []string{cfg}})
	if err != nil {
		t.Fatal(err)
	}
	urlText := "https://wkuser:" + cfg + "@example.com/x"
	if strings.Contains(withURL.Text(urlText), cfg) || !strings.Contains(withURL.Text(urlText), "example.com/x") {
		t.Fatalf("url config secret: %s", hide(withURL.Text(urlText), cfg))
	}
	allowedPass, err := New(Options{Allow: []string{cfg}})
	if err != nil {
		t.Fatal(err)
	}
	if got := allowedPass.Text(urlText); got != urlText {
		t.Fatalf("allowed password changed: %s", hide(got, cfg))
	}
	broken := withURL.Text("[REDACTED:NOT GOOD] " + cfg)
	if strings.Contains(broken, cfg) {
		t.Fatal("secret after a bad marker survived")
	}
}

func TestLoadSamplesBuildErrors(t *testing.T) {
	dir := t.TempDir()
	bodies := []string{
		`{"version":1,"samples":[{"name":"a","label":"jwt","build":"jwt"}]} trailing`,
		`{"version":1,"samples":[{"name":"a","label":"jwt","build":"repeat"}]}`,
		`{"version":1,"samples":[{"name":"a","label":"jwt","build":"literal"}]}`,
		`{"version":1,"samples":[{"name":"a","label":"jwt","build":"assignment","key":"password"}]}`,
		`{"version":1,"samples":[{"name":"a","label":"jwt","build":"json","key":"token"}]}`,
		`{"version":1,"samples":[{"name":"a","label":"jwt","build":"yaml","value":"x"}]}`,
		`{"version":1,"samples":[{"name":"a","label":"jwt","build":"url","user":"u","password":"p"}]}`,
		`{"version":1,"samples":[{"name":"a","label":"jwt","build":"bearer"}]}`,
		`{"version":1,"samples":[{"name":"a","label":"jwt","build":"pem","kind":"RSA PRIVATE KEY"}]}`,
	}
	for i, body := range bodies {
		path := filepath.Join(dir, fmt.Sprintf("bad-%d.json", i))
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSamples(path); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}
