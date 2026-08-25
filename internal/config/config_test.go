package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0x3ea/bigbrother/internal/config"
)

func TestParseTargets(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"normal", `[{"url":"https://a.com","interval_s":30},{"url":"https://b.com","interval_s":60}]`, false},
		{"empty array", `[]`, false},
		{"url empty", `[{"url":"","interval_s":30}]`, true},
		{"interval zero", `[{"url":"https://a.com","interval_s":0}]`, true},
		{"illegal json", `{]`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.ParseTargets([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTargets() failed=%v, expect=%v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadTargets(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T)
		wantErr bool
		wantLen int
	}{
		{
			name: "env points to a valid file",
			setup: func(t *testing.T) {
				f := filepath.Join(t.TempDir(), "targets.json")
				if err := os.WriteFile(f, []byte(`[{"url":"https://a.com","interval_s":30}]`), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv(config.TargetsEnvironment, f)
			},
			wantLen: 1,
		},
		{
			name: "env not set",
			setup: func(t *testing.T) {
				t.Setenv(config.TargetsEnvironment, "")
			},
			wantErr: true,
		},
		{
			name: "targets.json not exist",
			setup: func(t *testing.T) {
				t.Setenv(config.TargetsEnvironment, filepath.Join(t.TempDir(), "nope.json"))
			},
			wantErr: true,
		},
		{
			name: "env points to an illegal json file",
			setup: func(t *testing.T) {
				f := filepath.Join(t.TempDir(), "targets.json")
				if err := os.WriteFile(f, []byte(`{]`), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv(config.TargetsEnvironment, f)
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)

			got, err := config.LoadTargets()
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadTargets() err=%v, wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("len(got)=%d, want %d", len(got), tt.wantLen)
			}
		})
	}
}
