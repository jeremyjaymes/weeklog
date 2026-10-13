package config_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyjaymes/weeklog/internal/config"
)

func TestCheckResultDoesNotFormatSecrets(t *testing.T) {
	const (
		envSecret = "wksec_ENV_7f3a9c1e"
		literal   = "wksec_LITERAL_bb12dd34"
	)
	t.Setenv("WEEKLOG_TEST_SECRET", envSecret)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[output]
dir = "/tmp/weeklog-out"

[[source]]
type = "github"
token = "env:WEEKLOG_TEST_SECRET"

[[source]]
type = "basecamp"
account_id = "123"
token = "` + literal + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := config.Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	gotEnv, ok := result.Secret(0, "token")
	if !ok || gotEnv != envSecret {
		t.Fatal("env secret mismatch")
	}
	gotLiteral, ok := result.Secret(1, "token")
	if !ok || gotLiteral != literal {
		t.Fatal("literal secret mismatch")
	}
	if result.Config.Sources[1].Settings["token"] != "[literal]" {
		t.Fatal("literal setting was not redacted")
	}
	if result.Config.Sources[0].Settings["token"] != "env:WEEKLOG_TEST_SECRET" {
		t.Fatalf("env setting = %#v", result.Config.Sources[0].Settings["token"])
	}

	samples := []string{
		fmt.Sprintf("%v", result),
		fmt.Sprintf("%+v", result),
		fmt.Sprintf("%#v", result),
		fmt.Sprintf("%s", result),
		fmt.Sprintf("%q", result),
		result.String(),
		result.Report(),
		fmt.Sprintf("%v", result.Config),
		fmt.Sprintf("%+v", result.Config),
		fmt.Sprintf("%#v", result.Config),
		fmt.Sprintf("%s", result.Config),
	}
	for _, issue := range result.Issues {
		samples = append(samples, fmt.Sprintf("%+v", issue), issue.Message, issue.Field)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	configJSON, err := json.Marshal(result.Config)
	if err != nil {
		t.Fatal(err)
	}
	samples = append(samples, string(encoded), string(configJSON))

	for _, sample := range samples {
		for _, secret := range []string{envSecret, literal} {
			if strings.Contains(sample, secret) {
				t.Fatalf("formatted output contains secret value\n%s", strings.ReplaceAll(sample, secret, "[canary]"))
			}
		}
	}
	if !strings.Contains(result.Report(), "resolved (env:WEEKLOG_TEST_SECRET)") || !strings.Contains(result.Report(), "token: literal") {
		t.Fatalf("report did not describe secret status\n%s", result.Report())
	}
	values := result.SecretValues()
	if len(values) != 2 || values[0] != envSecret || values[1] != literal {
		t.Fatal("SecretValues mismatch")
	}
}
