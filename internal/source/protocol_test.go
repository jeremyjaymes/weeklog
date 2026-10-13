package source

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

func TestDecodePlanDescribe(t *testing.T) {
	const doc = `{
		"protocol": 1,
		"name": "basecamp",
		"version": "0.1.0",
		"config": {
			"account_id": {"type": "string", "required": true},
			"token": {"type": "secret", "required": true}
		}
	}`
	manifest, err := DecodeManifest([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "basecamp" || manifest.Protocol != 1 || manifest.Version != "0.1.0" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if !manifest.Config["account_id"].Required || manifest.Config["account_id"].Type != TypeString {
		t.Fatalf("account_id = %#v", manifest.Config["account_id"])
	}
	if manifest.Config["token"].Type != TypeSecret || !manifest.Config["token"].Required {
		t.Fatalf("token = %#v", manifest.Config["token"])
	}
}

func TestDecodeManifestRejects(t *testing.T) {
	tests := []string{
		`not json`,
		`{"protocol":1,"name":"ok","version":"0.1.0","config":{}}{"extra":true}`,
		`{"protocol":2,"name":"ok","version":"0.1.0","config":{}}`,
		`{"protocol":1,"name":"Bad Name","version":"0.1.0","config":{}}`,
		`{"protocol":1,"name":"ok","version":"","config":{}}`,
		`{"protocol":1,"name":"ok","version":"0.1.0","config":{"token":{"type":"file"}}}`,
	}
	for _, doc := range tests {
		_, err := DecodeManifest([]byte(doc))
		if !errors.Is(err, ErrProtocol) {
			t.Fatalf("doc %s err = %v, want protocol error", doc, err)
		}
	}
}

func TestEncodeRequestOmitsSecretsFromError(t *testing.T) {
	const secret = "wksec_REQUEST_should_not_appear"
	_, err := EncodeRequest(CollectRequest{
		Protocol: 1,
		Since:    time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		Until:    time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Config:   map[string]any{"token": secret},
	})
	if !errors.Is(err, ErrConfig) {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("validation error included a secret")
	}
}

func TestRequestCursorNullAndIdentity(t *testing.T) {
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, time.FixedZone("CDT", -5*3600))
	until := since.Add(7 * 24 * time.Hour)
	buf, err := EncodeRequest(CollectRequest{
		Since: since,
		Until: until,
		Identity: Identity{
			Emails:  []string{"jeremy@example.com"},
			Handles: map[string]string{"github": "jeremyjaymes"},
		},
		Config: map[string]any{"account_id": "123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf, []byte(`"cursor":null`)) {
		t.Fatalf("request = %s", buf)
	}
	if !bytes.Contains(buf, []byte(`"protocol":1`)) || !bytes.Contains(buf, []byte(`"github":"jeremyjaymes"`)) {
		t.Fatalf("request = %s", buf)
	}
	req, err := DecodeRequest(buf)
	if err != nil {
		t.Fatal(err)
	}
	if req.Cursor != nil || req.Config["account_id"] != "123" {
		t.Fatalf("decoded = %#v", req)
	}
	if len(req.Identity.Emails) != 1 || req.Identity.Handles["github"] != "jeremyjaymes" {
		t.Fatalf("identity = %#v", req.Identity)
	}
}

func TestAcceptEventFillsSourceAndDropsMismatch(t *testing.T) {
	raw := []byte(`{"source_id":"e1","kind":"COMMIT","ts":"2026-10-08T14:02:11Z","title":"Fix"}`)
	ev, err := acceptEvent("git", raw)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Source != "git" || ev.Kind != event.KindCommit {
		t.Fatalf("event = %#v", ev)
	}
	if _, err := acceptEvent("git", []byte(`{"source":"other","source_id":"e1","kind":"commit","ts":"2026-10-08T14:02:11Z"}`)); err == nil {
		t.Fatal("expected source mismatch to fail")
	}
	if _, err := acceptEvent("git", []byte(`{"source":"git","kind":"commit","ts":"2026-10-08T14:02:11Z"}`)); err == nil {
		t.Fatal("expected missing source_id to fail")
	}
}

func TestBuiltinCollectIsNotImplemented(t *testing.T) {
	_, err := Unimplemented{Name: "git"}.Collect(context.Background(), CollectRequest{}, nil)
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
}
