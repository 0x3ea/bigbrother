package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0x3ea/bigbrother/internal/config"
)

func TestParseTargets(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantErrSub string // 空 = 期望成功;非空 = 期望错误信息包含该子串
	}{
		// --- 合法 ---
		{"http_target", `[{"name":"a","type":"http","target":"http://a.com","interval_ms":30,"timeout_ms":5000}]`, ""},
		{"https_target", `[{"name":"a","type":"https","target":"https://a.com","interval_ms":30,"timeout_ms":5000}]`, ""},
		{"tcp_target", `[{"name":"baidu","type":"tcp","target":"baidu.com:443","interval_ms":10,"timeout_ms":2000}]`, ""},
		{"type_case_insensitive", `[{"name":"a","type":"HTTPS","target":"https://a.com","interval_ms":30,"timeout_ms":5000}]`, ""},
		{"multiple_targets", `[{"name":"a","type":"http","target":"http://a.com","interval_ms":30,"timeout_ms":5000},{"name":"b","type":"tcp","target":"a.com:443","interval_ms":10,"timeout_ms":2000}]`, ""},
		{"empty_array", `[]`, ""},

		// --- JSON 层 ---
		{"illegal_json", `{]`, "parse targets"},

		// --- type 校验(最先执行,会掩盖后面的错误,所以单独覆盖) ---
		{"type_missing", `[{"name":"a","target":"http://a.com","interval_ms":30,"timeout_ms":5000}]`, "prober type"},
		{"type_unknown", `[{"name":"a","type":"grpc","target":"a.com:443","interval_ms":30,"timeout_ms":5000}]`, "prober type"},

		// --- http/https target 格式 ---
		{"http_target_no_host", `[{"name":"a","type":"http","target":"http://","interval_ms":30,"timeout_ms":5000}]`, "missing host"},
		// url.Parse 极其宽容,裸字符串也能解析成功,靠 Host=="" 才拦得住
		{"http_target_bare_string", `[{"name":"a","type":"http","target":"hello","interval_ms":30,"timeout_ms":5000}]`, "missing host"},

		// --- tcp target 格式 ---
		{"tcp_target_no_port", `[{"name":"example","type":"tcp","target":"example.com","interval_ms":30,"timeout_ms":5000}]`, "tcp target"},
		{"tcp_target_port_out_of_range", `[{"name":"example","type":"tcp","target":"example.com:70000","interval_ms":30,"timeout_ms":5000}]`, "tcp target"},
		{"tcp_target_no_host", `[{"name":"a","type":"tcp","target":":443","interval_ms":30,"timeout_ms":5000}]`, "tcp target"},

		// --- 数值校验(必须带合法 type,否则会被 type 检查提前拦下) ---
		{"interval_zero", `[{"name":"a","type":"http","target":"http://a.com","interval_ms":0,"timeout_ms":5000}]`, "interval_ms"},
		{"timeout_zero", `[{"name":"a","type":"http","target":"http://a.com","interval_ms":30,"timeout_ms":0}]`, "timeout_ms"},

		// --- 不能有重名 ---
		{"duplicate_name", `[{"name":"a","type":"tcp","target":"baidu.com:443","interval_ms":1,"timeout_ms":1},
		{"name":"a","type":"tcp","target":"qq.com:443","interval_ms":1,"timeout_ms":1}
		]`, "duplicated"},

		// --- 缺失name ---
		{"name_missing", `[{"name":"","type":"tcp","target":"baidu.com:443","interval_ms":1,"timeout_ms":1}
		]`, "missing name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.ParseTargets([]byte(tt.input))
			switch {
			case tt.wantErrSub == "" && err != nil:
				t.Fatalf("ParseTargets() unexpected error: %v", err)
			case tt.wantErrSub != "" && err == nil:
				t.Fatalf("ParseTargets() expected error containing %q, got nil", tt.wantErrSub)
			case tt.wantErrSub != "" && !strings.Contains(err.Error(), tt.wantErrSub):
				t.Errorf("error = %v, want containing %q", err, tt.wantErrSub)
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
		want    *config.Target // 非 nil 时校验第一条解析结果(JSON tag 回环检查)
	}{
		{
			name: "env points to a valid file",
			setup: func(t *testing.T) {
				f := filepath.Join(t.TempDir(), "targets.json")
				data := `[{"name":"a","type":"https","target":"https://a.com","interval_ms":3000,"timeout_ms":5000}]`
				if err := os.WriteFile(f, []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv(config.TargetsEnvironment, f)
			},
			wantLen: 1,
			want:    &config.Target{Name: "a", Type: "https", Target: "https://a.com", IntervalMS: 3000, TimeoutMS: 5000},
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
		{
			name: "env points to a file with invalid target",
			setup: func(t *testing.T) {
				f := filepath.Join(t.TempDir(), "targets.json")
				// JSON 合法,但 http target 缺 host —— 校验层必须拦住
				data := `[{"name":"a","type":"http","target":"http://","interval_ms":3000,"timeout_ms":5000}]`
				if err := os.WriteFile(f, []byte(data), 0o644); err != nil {
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
			// 字段改名(url→target、新增 type)最容易错在 JSON tag 上,做一次回环检查
			if tt.want != nil && got[0] != *tt.want {
				t.Errorf("parsed target = %+v, want %+v", got[0], tt.want)
			}
		})
	}
}
