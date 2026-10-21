package rtsp

import (
	"strings"
	"testing"

	"tuya-ipc-terminal/pkg/storage"
)

func TestGenerateSDPUsesSelectedVideoResolution(t *testing.T) {
	camera := &storage.CameraInfo{
		Skill: `{"videos":[{"streamType":2,"codecType":4,"width":2560,"height":1440},{"streamType":4,"codecType":2,"width":640,"height":360}]}`,
	}
	server := &RTSPServer{}

	tests := []struct {
		name       string
		resolution string
		want       string
		wantNot    string
	}{
		{name: "HD HEVC", resolution: "hd", want: "m=video 0 RTP/AVP 100\r\na=rtpmap:100 H265/90000"},
		{name: "SD H264", resolution: "sd", want: "m=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000", wantNot: "H265/90000"},
		{name: "empty resolution defaults to HD", want: "m=video 0 RTP/AVP 100\r\na=rtpmap:100 H265/90000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sdp := server.generateSDP(camera, "rtsp://camera/stream", tt.resolution)
			if !strings.Contains(sdp, tt.want) {
				t.Fatalf("SDP does not contain selected video format %q:\n%s", tt.want, sdp)
			}
			if tt.wantNot != "" && strings.Contains(sdp, tt.wantNot) {
				t.Fatalf("SDP unexpectedly contains %q:\n%s", tt.wantNot, sdp)
			}
		})
	}
}
