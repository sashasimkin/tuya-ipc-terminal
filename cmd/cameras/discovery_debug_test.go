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
		name, owned, shared, failPath, failure, include                  string
		duplicate, quiet, supports, badSkill, wantError, seed            bool
		badCodec, badDimensions, badStream, hevc, failOne, mixedProfiles bool
		secondAccount, repeatedRoom                                      bool
		videos, want, wantCalls                                          int
		traces                                                           []string
	}{
		{name: "empty", traces: []string{"stage=homes count=0", "stage=shared groups=0", "stage=summary candidates=0 cameras=0"}},
		{name: "owned-sp_wnq", owned: "sp_wnq", supports: true, videos: 1, want: 1},
		{name: "shared-sp_wnq", shared: "sp_wnq", supports: true, videos: 1, want: 1},
		{name: "shared-sp", shared: "sp", supports: true, videos: 1, want: 1},
		{name: "shared-dghsxj", shared: "dghsxj", supports: true, videos: 1, want: 1},
		{name: "owned-dghsxj", owned: "dghsxj", supports: true, videos: 1, want: 1},
		{name: "non-camera", owned: "switch", shared: "switch", traces: []string{"supports_webrtc=false", "decision=unsupported_capability", "stage=summary candidates=2 cameras=0"}},
		{name: "duplicate", owned: "sp", shared: "sp", duplicate: true, supports: true, videos: 1, want: 1, traces: []string{"source=shared category=sp decision=duplicate", "stage=summary candidates=1 cameras=1"}},
		{name: "home-http-error-with-shared-camera-preserves-registry", shared: "sp", supports: true, videos: 1, failPath: "/api/new/common/homeList", failure: "http", seed: true, wantError: true, want: 1, traces: []string{"stage=homes http_status=503", "stage=homes failure=http", "stage=registry decision=preserved"}},
		{name: "room-api-error-preserves-registry", owned: "sp", failPath: "/api/new/common/roomList", failure: "api", seed: true, wantError: true, want: 1, traces: []string{"stage=rooms failure=api", "stage=registry decision=preserved"}},
		{name: "shared-malformed-json-preserves-registry", failPath: "/api/new/playback/shareList", failure: "json", seed: true, wantError: true, want: 1, traces: []string{"stage=shared failure=json"}},
		{name: "config-api-error-preserves-registry", owned: "sp", failPath: "/api/jarvis/config", failure: "api", seed: true, wantError: true, want: 1, traces: []string{"stage=webrtc failure=api"}},
		{name: "config-http-error-preserves-registry", owned: "sp", failPath: "/api/jarvis/config", failure: "http", seed: true, wantError: true, want: 1},
		{name: "config-json-error-preserves-registry", owned: "sp", failPath: "/api/jarvis/config", failure: "json", seed: true, wantError: true, want: 1},
		{name: "config-transport-error-preserves-registry", owned: "sp", failPath: "/api/jarvis/config", failure: "transport", seed: true, wantError: true, want: 1},
		{name: "session-invalid", failPath: "/api/customized/web/app/info", failure: "api", seed: true, wantError: true, want: 1, traces: []string{"stage=session failure=api"}},
		{name: "transport-failure", failPath: "/api/new/common/homeList", failure: "transport", seed: true, wantError: true, want: 1, traces: []string{"stage=homes failure=transport"}},
		{name: "unsafe-category", owned: "sp\n" + secret, supports: true, videos: 1, want: 1, traces: []string{"source=owned category=redacted decision=accepted"}},
		{name: "debug-is-opt-in", owned: "sp", quiet: true, supports: true, videos: 1, want: 1},
		{name: "custom-owned-discovered-by-default", owned: "custom_camera", supports: true, videos: 1, want: 1, traces: []string{"category=custom_camera decision=accepted", "supports_webrtc=true video_profiles=1"}},
		{name: "custom-shared-discovered-by-default", shared: "custom_camera", supports: true, videos: 1, want: 1},
		{name: "invalid-category-still-probed", owned: " ", supports: true, videos: 1, want: 1},
		{name: "custom-hevc", owned: "custom_camera", supports: true, videos: 1, hevc: true, want: 1},
		{name: "deprecated-include-compatible", owned: "custom_camera", include: "custom_camera", supports: true, videos: 1, want: 1},
		{name: "custom-deduplicated", owned: "custom_camera", shared: "custom_camera", duplicate: true, supports: true, videos: 1, want: 1},
		{name: "known-category-without-webrtc-rejected", owned: "sp", videos: 1, traces: []string{"decision=unsupported_capability"}},
		{name: "known-category-without-video-rejected", owned: "sp", supports: true, traces: []string{"decision=unsupported_capability"}},
		{name: "custom-without-webrtc-rejected", owned: "custom_camera", videos: 1},
		{name: "custom-without-video-rejected", owned: "custom_camera", supports: true},
		{name: "unsupported-codec", owned: "custom_camera", supports: true, videos: 1, badCodec: true},
		{name: "invalid-dimensions", owned: "custom_camera", supports: true, videos: 1, badDimensions: true},
		{name: "invalid-stream", owned: "custom_camera", supports: true, videos: 1, badStream: true},
		{name: "malformed-skill-preserves-registry", owned: "custom_camera", supports: true, badSkill: true, seed: true, wantError: true, want: 1, traces: []string{"stage=skill failure=json"}},
		{name: "successful-empty-refresh-removes-stale-camera", seed: true},
		{name: "confirmed-unsupported-removes-stale-camera", owned: "custom_camera", seed: true},
		{name: "config-error-without-debug", owned: "sp", failPath: "/api/jarvis/config", failure: "api", seed: true, wantError: true, quiet: true, want: 1},
		{name: "one-config-error-among-successes-preserves-registry", owned: "custom_camera", shared: "custom_camera", supports: true, videos: 1, failPath: "/api/jarvis/config", failure: "api", failOne: true, seed: true, wantError: true, want: 1, wantCalls: 2, traces: []string{"stage=summary candidates=2 cameras=1"}},
		{name: "mixed-supported-and-unsupported-profiles-rejected", owned: "custom_camera", supports: true, videos: 2, mixedProfiles: true},
		{name: "failed-account-preserved-healthy-account-updates", owned: "custom_camera", supports: true, videos: 1, failPath: "/api/jarvis/config", failure: "api", seed: true, wantError: true, want: 1, secondAccount: true},
		{name: "duplicate-across-rooms", owned: "custom_camera", supports: true, videos: 1, want: 1, wantCalls: 1, repeatedRoom: true},
		{name: "invalid-include-redacted", include: "custom_camera\n" + secret, wantError: true},
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
			users, err := manager.ListUsers()
			if err != nil {
				t.Fatal(err)
			}
			userKey := users[0].UserKey
			if tc.secondAccount {
				if err := manager.SaveUser("eu-central", "second@example.invalid", &tuya.SessionData{ServerHost: "healthy.example.invalid"}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.seed {
				if err := manager.UpdateCamerasForUser(userKey, []storage.CameraInfo{{UserKey: userKey, DeviceID: "ExistingID", RTSPPath: "ExistingPath"}}); err != nil {
					t.Fatal(err)
				}
			}
			if err := manager.UpdateCamerasForUser("other-account", []storage.CameraInfo{{UserKey: "other-account", DeviceID: "OtherID"}}); err != nil {
				t.Fatal(err)
			}
			before, err := manager.GetCameraRegistry()
			if err != nil {
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
				if req.Method != "POST" || (req.URL.Host != "tuya.example.invalid" && req.URL.Host != "healthy.example.invalid") {
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
					rooms := []map[string]interface{}{{"deviceList": device(tc.owned, "PrivateOwnedID")}}
					if tc.repeatedRoom {
						rooms = append(rooms, rooms[0])
					}
					result = rooms
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
					videos := make([]map[string]int, tc.videos)
					for i := range videos {
						codec, width, stream := 2, 1920, 2
						if tc.hevc {
							codec = 4
						}
						if tc.badCodec || (tc.mixedProfiles && i == 1) {
							codec = 99
						}
						if tc.badDimensions {
							width = 0
						}
						if tc.badStream {
							stream = 0
						}
						videos[i] = map[string]int{"codecType": codec, "streamType": stream, "width": width, "height": 1080}
					}
					skill, err := json.Marshal(map[string]interface{}{"videos": videos})
					if err != nil {
						t.Fatal(err)
					}
					if tc.badSkill {
						skill = []byte(secret)
					}
					result = map[string]interface{}{"skill": string(skill), "supportsWebrtc": tc.supports}
				default:
					t.Fatalf("unexpected path: %s", req.URL.Path)
				}
				body, err := json.Marshal(map[string]interface{}{"success": true, "result": result})
				if err != nil {
					t.Fatal(err)
				}
				status := 200
				failThisRequest := req.URL.Path == tc.failPath && req.URL.Host != "healthy.example.invalid"
				if failThisRequest && tc.failOne {
					var data map[string]string
					if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
						t.Fatal(err)
					}
					failThisRequest = data["devId"] == "PrivateOwnedID"
				}
				if failThisRequest {
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
			if tc.include != "" {
				args = append(args, "--include-category", tc.include)
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); tc.wantError {
				if err == nil || strings.Contains(err.Error(), secret) {
					t.Fatalf("expected redacted error, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			got, err := manager.GetCamerasForUser(userKey)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want {
				t.Fatalf("cameras=%d; want %d", len(got), tc.want)
			}
			if tc.wantError {
				after, err := manager.GetCameraRegistry()
				if err != nil {
					t.Fatal(err)
				}
				beforeJSON, _ := json.Marshal(before)
				afterJSON, _ := json.Marshal(after)
				if tc.secondAccount {
					var prior []storage.CameraInfo
					for _, camera := range before.Cameras {
						if camera.UserKey == userKey {
							prior = append(prior, camera)
						}
					}
					beforeJSON, _ = json.Marshal(prior)
					afterJSON, _ = json.Marshal(got)
					users, err := manager.ListUsers()
					if err != nil {
						t.Fatal(err)
					}
					for _, user := range users {
						if user.UserKey != userKey {
							cams, err := manager.GetCamerasForUser(user.UserKey)
							if err != nil || len(cams) != 1 || cams[0].DeviceID != "PrivateOwnedID" {
								t.Fatal("healthy account was not updated")
							}
						}
					}
				}
				if !bytes.Equal(beforeJSON, afterJSON) {
					t.Fatal("failed discovery changed the persisted registry")
				}
			}
			other, err := manager.GetCamerasForUser("other-account")
			if err != nil || len(other) != 1 || other[0].DeviceID != "OtherID" {
				t.Fatal("refresh changed another account")
			}
			if tc.wantCalls != 0 && configCalls != tc.wantCalls {
				t.Fatalf("config calls=%d; want %d", configCalls, tc.wantCalls)
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
