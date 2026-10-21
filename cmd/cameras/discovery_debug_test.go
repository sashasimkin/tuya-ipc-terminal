package cameras_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"tuya-ipc-terminal/cmd/cameras"
	"tuya-ipc-terminal/pkg/storage"
	"tuya-ipc-terminal/pkg/tuya"
)

type mockTransport func(*http.Request) (*http.Response, error)

func (f mockTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRefreshDebug(t *testing.T) {
	const secret = "PRIVATE-cookie-and-api-message"
	cases := []struct {
		name, owned, shared, failPath, failure string
		duplicate, quiet                       bool
		want                                   int
		traces                                 []string
	}{
		{name: "empty", traces: []string{"stage=homes count=0", "stage=shared groups=0", "stage=summary candidates=0 cameras=0"}},
		{name: "owned-sp_wnq", owned: "sp_wnq", want: 1, traces: []string{"source=owned category=sp_wnq decision=accepted", "stage=summary candidates=1 cameras=1"}},
		{name: "shared-sp_wnq-is-still-filtered", shared: "sp_wnq", traces: []string{"source=shared category=sp_wnq decision=unsupported"}},
		{name: "shared-sp", shared: "sp", want: 1, traces: []string{"source=shared category=sp decision=accepted"}},
		{name: "shared-dghsxj", shared: "dghsxj", want: 1, traces: []string{"source=shared category=dghsxj decision=accepted"}},
		{name: "owned-dghsxj", owned: "dghsxj", want: 1, traces: []string{"source=owned category=dghsxj decision=accepted"}},
		{name: "non-camera", owned: "switch", shared: "switch", traces: []string{"source=owned category=switch decision=unsupported", "source=shared category=switch decision=unsupported"}},
		{name: "duplicate", owned: "sp", shared: "sp", duplicate: true, want: 1, traces: []string{"source=shared category=sp decision=duplicate", "stage=summary candidates=1 cameras=1"}},
		{name: "home-http-error-with-shared-fallback", shared: "sp", failPath: "/api/new/common/homeList", failure: "http", want: 1, traces: []string{"stage=homes http_status=503", "stage=homes failure=http", "stage=summary candidates=1 cameras=1"}},
		{name: "room-api-error", owned: "sp", failPath: "/api/new/common/roomList", failure: "api", traces: []string{"stage=rooms failure=api", "stage=summary candidates=0 cameras=0"}},
		{name: "shared-malformed-json", failPath: "/api/new/playback/shareList", failure: "json", traces: []string{"stage=shared failure=json"}},
		{name: "config-api-error", owned: "sp", failPath: "/api/jarvis/config", failure: "api", traces: []string{"stage=webrtc failure=api", "stage=summary candidates=1 cameras=0"}},
		{name: "session-invalid", failPath: "/api/customized/web/app/info", failure: "api", traces: []string{"stage=session failure=api"}},
		{name: "transport-failure", failPath: "/api/new/common/homeList", failure: "transport", traces: []string{"stage=homes failure=transport"}},
		{name: "unsafe-category", owned: "sp\n" + secret, traces: []string{"source=owned category=redacted decision=unsupported"}},
		{name: "debug-is-opt-in", owned: "sp", quiet: true, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(t.TempDir()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chdir(cwd); err != nil {
					t.Error(err)
				}
			})
			manager, err := storage.NewStorageManager()
			if err != nil {
				t.Fatal(err)
			}
			cameras.SetStorageManager(manager)
			if err := manager.SaveUser("eu-central", "private@example.invalid", &tuya.SessionData{
				ServerHost: "tuya.example.invalid",
				Cookies:    []*tuya.Cookie{{Name: "sid", Value: secret, Path: "/", Secure: true}},
			}); err != nil {
				t.Fatal(err)
			}
			device := func(category, id string) []map[string]string {
				if category == "" {
					return []map[string]string{}
				}
				return []map[string]string{{"category": category, "deviceId": id, "deviceName": "PrivateCamera", "productId": "PrivateProduct", "uuid": "PrivateUUID"}}
			}
			configCalls := 0
			oldTransport := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = oldTransport })
			http.DefaultTransport = mockTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != "POST" || req.URL.Host != "tuya.example.invalid" {
					t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
				}
				var result interface{}
				switch req.URL.Path {
				case "/api/customized/web/app/info":
					result = map[string]string{"appName": "Test"}
				case "/api/new/common/homeList":
					result = []map[string]int{}
					if tc.owned != "" {
						result = []map[string]int{{"gid": 123456}}
					}
				case "/api/new/common/roomList":
					result = []map[string]interface{}{{"deviceList": device(tc.owned, "PrivateOwnedID")}}
				case "/api/new/playback/shareList":
					groups := []map[string]interface{}{}
					if tc.shared != "" {
						id := "PrivateSharedID"
						if tc.duplicate {
							id = "PrivateOwnedID"
						}
						groups = append(groups, map[string]interface{}{"deviceInfoList": device(tc.shared, id)})
					}
					result = map[string]interface{}{"securityWebCShareInfoList": groups}
				case "/api/jarvis/config":
					configCalls++
					result = map[string]string{"skill": "PrivateSkill"}
				default:
					t.Fatalf("unexpected path: %s", req.URL.Path)
				}
				body, err := json.Marshal(map[string]interface{}{"success": true, "result": result})
				if err != nil {
					t.Fatal(err)
				}
				status := 200
				if req.URL.Path == tc.failPath {
					switch tc.failure {
					case "http":
						status = 503
						body = []byte(secret)
					case "api":
						body = []byte(`{"success":false,"errorMsg":"` + secret + `"}`)
					case "json":
						body = []byte("not-json-" + secret)
					case "transport":
						return nil, fmt.Errorf("%s", secret)
					}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
			})
			stdout, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stdout.Close() })
			oldStdout := os.Stdout
			os.Stdout = stdout
			t.Cleanup(func() { os.Stdout = oldStdout })
			var diagnostics bytes.Buffer
			cmd := cameras.NewCamerasCmd()
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetErr(&diagnostics)
			args := []string{"refresh"}
			if !tc.quiet {
				args = append(args, "--debug")
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			got, err := manager.GetAllCameras()
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want {
				t.Fatalf("cameras=%d; want %d", len(got), tc.want)
			}
			if tc.duplicate && configCalls != 1 {
				t.Fatalf("duplicate caused %d config requests", configCalls)
			}
			trace := diagnostics.String()
			if tc.quiet && trace != "" {
				t.Fatalf("debug is not opt-in: %s", trace)
			}
			for _, want := range tc.traces {
				if !strings.Contains(trace, want) {
					t.Errorf("missing %q in diagnostics:\n%s", want, trace)
				}
			}
			if !tc.quiet {
				output, err := os.ReadFile(stdout.Name())
				if err != nil {
					t.Fatal(err)
				}
				for _, private := range []string{secret, "private@example.invalid", "PrivateOwnedID", "PrivateSharedID", "PrivateCamera", "PrivateProduct", "PrivateUUID", "PrivateSkill", "123456", "tuya.example.invalid"} {
					if strings.Contains(trace+string(output), private) {
						t.Errorf("debug output leaked %q", private)
					}
				}
			}
		})
	}
}
