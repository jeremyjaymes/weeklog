package redact

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Sample is one fake secret from the redaction fixture corpus.
// Secret and AlsoBan must not survive redaction. Keep must survive.
// NeedsKey is true when Secret is removed only if it is passed as a config secret.
type Sample struct {
	Name     string
	Label    string
	Secret   string
	Text     string
	AlsoBan  []string
	Keep     []string
	NeedsKey bool
}

type corpusFile struct {
	Version int          `json:"version"`
	Samples []sampleSpec `json:"samples"`
}

type sampleSpec struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Build    string `json:"build"`
	Prefix   string `json:"prefix"`
	Char     string `json:"char"`
	Count    int    `json:"count"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	User     string `json:"user"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Kind     string `json:"kind"`
	Body     string `json:"body"`
	NeedsKey bool   `json:"needs_key"`
}

// LoadSamples reads the fake-secret corpus. The file describes how to build
// each secret so the fixture itself does not contain a contiguous token.
func LoadSamples(path string) ([]Sample, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read corpus: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var file corpusFile
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("parse corpus: %w", err)
	}
	var trailing struct{}
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("parse corpus: trailing data")
		}
		return nil, fmt.Errorf("parse corpus: %w", err)
	}
	if file.Version != 1 {
		return nil, fmt.Errorf("corpus version %d", file.Version)
	}
	if len(file.Samples) == 0 {
		return nil, fmt.Errorf("corpus has no samples")
	}
	seen := make(map[string]struct{}, len(file.Samples))
	out := make([]Sample, 0, len(file.Samples))
	for i, spec := range file.Samples {
		if spec.Name == "" {
			return nil, fmt.Errorf("samples[%d]: name is empty", i)
		}
		if _, dup := seen[spec.Name]; dup {
			return nil, fmt.Errorf("samples[%d]: duplicate name %s", i, spec.Name)
		}
		seen[spec.Name] = struct{}{}
		if !markerKindRE.MatchString(spec.Label) {
			return nil, fmt.Errorf("samples[%d]: invalid label", i)
		}
		sample, err := buildSample(spec)
		if err != nil {
			return nil, fmt.Errorf("samples[%d] %s: %w", i, spec.Name, err)
		}
		if sample.Secret == "" || sample.Text == "" || !strings.Contains(sample.Text, sample.Secret) {
			return nil, fmt.Errorf("samples[%d] %s: empty secret", i, spec.Name)
		}
		out = append(out, sample)
	}
	return out, nil
}

func buildSample(spec sampleSpec) (Sample, error) {
	sample := Sample{
		Name:     spec.Name,
		Label:    spec.Label,
		NeedsKey: spec.NeedsKey,
	}
	switch spec.Build {
	case "repeat":
		if spec.Prefix == "" || len(spec.Char) != 1 || spec.Count <= 0 {
			return Sample{}, fmt.Errorf("repeat requires prefix, one-byte char, and count")
		}
		sample.Secret = spec.Prefix + strings.Repeat(spec.Char, spec.Count)
		sample.Text = sample.Secret
	case "literal":
		if spec.Value == "" {
			return Sample{}, fmt.Errorf("literal requires value")
		}
		sample.Secret = spec.Value
		sample.Text = spec.Value
	case "assignment":
		if spec.Key == "" || spec.Value == "" {
			return Sample{}, fmt.Errorf("assignment requires key and value")
		}
		sample.Secret = spec.Value
		sample.Text = spec.Key + "=" + spec.Value
	case "json":
		if spec.Key == "" || spec.Value == "" {
			return Sample{}, fmt.Errorf("json requires key and value")
		}
		sample.Secret = spec.Value
		sample.Text = `"` + spec.Key + `": "` + spec.Value + `"`
	case "yaml":
		if spec.Key == "" || spec.Value == "" {
			return Sample{}, fmt.Errorf("yaml requires key and value")
		}
		sample.Secret = spec.Value
		sample.Text = spec.Key + ": " + spec.Value
	case "url":
		if spec.User == "" || spec.Password == "" || spec.Host == "" {
			return Sample{}, fmt.Errorf("url requires user, password, and host")
		}
		sample.Secret = spec.Password
		sample.Text = "https://" + spec.User + ":" + spec.Password + "@" + spec.Host
		sample.AlsoBan = []string{spec.User + ":" + spec.Password}
		sample.Keep = []string{spec.Host}
	case "bearer":
		if spec.Value == "" {
			return Sample{}, fmt.Errorf("bearer requires value")
		}
		sample.Secret = spec.Value
		sample.Text = "Bearer " + spec.Value
	case "pem":
		if spec.Kind == "" || spec.Body == "" {
			return Sample{}, fmt.Errorf("pem requires kind and body")
		}
		sample.Secret = spec.Body
		sample.Text = "-----BEGIN " + spec.Kind + "-----\n" + spec.Body + "\n-----END " + spec.Kind + "-----"
	case "jwt":
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
		payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"weeklog-fake-subject"}`))
		sig := strings.Repeat("a", 16)
		token := header + "." + payload + "." + sig
		sample.Secret = token
		sample.Text = token
		sample.AlsoBan = []string{payload, sig}
	case "alphabet":
		// Standard base64 alphabet. Every character is unique, so Shannon
		// entropy is 6, and the letters past "f" keep it out of the hex exclusion.
		text := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
		sample.Secret = text
		sample.Text = text
	case "aws-secret":
		value := strings.Repeat("Aa1+", 10)
		if len(value) != 40 {
			return Sample{}, fmt.Errorf("aws secret length %d", len(value))
		}
		sample.Secret = value
		sample.Text = "aws_secret_access_key=" + value
	case "slack-webhook":
		team := "T" + strings.Repeat("A", 9)
		channel := "B" + strings.Repeat("C", 9)
		tail := strings.Repeat("D", 24)
		sample.Text = "https://hooks.slack.com/services/" + team + "/" + channel + "/" + tail
		sample.Secret = team + "/" + channel + "/" + tail
		sample.AlsoBan = []string{team, tail}
	default:
		return Sample{}, fmt.Errorf("unknown build %q", spec.Build)
	}
	return sample, nil
}
