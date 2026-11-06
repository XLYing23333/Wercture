package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type mediaFormat struct{ mime, demuxer string }

var mediaFormats = map[string]mediaFormat{
	".mp4": {"video/mp4", "mov"}, ".webm": {"video/webm", "matroska"},
	".mp3": {"audio/mpeg", "mp3"}, ".m4a": {"audio/mp4", "mov"},
	".aac": {"audio/aac", "aac"}, ".wav": {"audio/wav", "wav"},
	".ogg": {"audio/ogg", "ogg"}, ".flac": {"audio/flac", "flac"},
}

type probeResult struct {
	Streams []struct {
		CodecType         string `json:"codec_type"`
		Width             int    `json:"width"`
		Height            int    `json:"height"`
		SampleAspectRatio string `json:"sample_aspect_ratio"`
		Disposition       struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
		SideData []struct {
			Rotation float64 `json:"rotation"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// boundedOutput prevents corrupt metadata from growing subprocess output indefinitely.
type boundedOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, errors.New("media output too large")
	}
	return b.buffer.Write(p)
}

func runMedia(ctx context.Context, tool string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, tool, args...)
	out := &boundedOutput{limit: 1 << 20}
	cmd.Stdout = out
	// Diagnostics stay private; uploaded metadata must not enter server logs.
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.buffer.Bytes(), nil
}

func (s *Server) inspectMedia(parent context.Context, file, name string, header []byte) (kind string, width, height int, err error) {
	format, ok := mediaFormats[strings.ToLower(filepath.Ext(name))]
	if !ok {
		return "", 0, 0, errBadType
	}
	// WebM and Matroska share a demuxer; only accept WebM's EBML document type.
	if format.mime == "video/webm" && !bytes.Contains(header, []byte("webm")) {
		return "", 0, 0, errBadType
	}
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	select {
	case s.mediaSlots <- struct{}{}:
		defer func() { <-s.mediaSlots }()
	case <-ctx.Done():
		return "", 0, 0, ctx.Err()
	}
	raw, err := runMedia(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file", "-f", format.demuxer, "-show_streams", "-show_format", "-of", "json", file)
	if err != nil {
		return "", 0, 0, errBadType
	}
	var probe probeResult
	if json.Unmarshal(raw, &probe) != nil {
		return "", 0, 0, errBadType
	}
	video, audio := false, false
	for _, stream := range probe.Streams {
		if stream.CodecType == "audio" {
			audio = true
		}
		if stream.CodecType == "video" && stream.Disposition.AttachedPic == 0 && !video {
			video = true
			width, height = stream.Width, stream.Height
			var n, d int
			if _, e := fmt.Sscanf(stream.SampleAspectRatio, "%d:%d", &n, &d); e == nil && n > 0 && d > 0 {
				width = int(math.Round(float64(width) * float64(n) / float64(d)))
			}
			for _, side := range stream.SideData {
				if int(math.Abs(side.Rotation))%180 == 90 {
					width, height = height, width
					break
				}
			}
		}
	}
	if strings.HasPrefix(format.mime, "audio/") {
		if !audio || video {
			return "", 0, 0, errBadType
		}
		return format.mime, 512, 512, nil
	}
	if !video || width < 1 || height < 1 {
		return "", 0, 0, errBadType
	}
	duration, _ := strconv.ParseFloat(probe.Format.Duration, 64)
	if math.IsNaN(duration) || math.IsInf(duration, 0) || duration < 0 {
		duration = 0
	}
	preview := file + ".preview.png"
	args := []string{"-v", "error", "-nostdin", "-protocol_whitelist", "file", "-f", format.demuxer, "-ss", strconv.FormatFloat(duration*0.1, 'f', 6, 64), "-i", file, "-map", "0:V:0", "-frames:v", "1", "-vf", "scale=w='max(1,round(iw*sar*min(1,min(640/(iw*sar),640/ih))))':h='max(1,round(ih*min(1,min(640/(iw*sar),640/ih))))',setsar=1", "-threads", "1", "-f", "image2", "-y", preview}
	if _, err := runMedia(ctx, "ffmpeg", args...); err != nil {
		_ = os.Remove(preview)
		return "", 0, 0, errBadType
	}
	f, err := os.Open(preview)
	if err != nil {
		return "", 0, 0, errBadType
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 640 || cfg.Height > 640 {
		_ = os.Remove(preview)
		return "", 0, 0, errBadType
	}
	return format.mime, width, height, nil
}
