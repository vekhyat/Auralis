package ipod

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vekhyat/Auralis/backend"
)

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("copy: %w", firstErr(copyErr, syncErr, closeErr))
	}
	return nil
}

func transcodeFile(src, dst, format string) error {
	ffmpeg, err := backend.GetFFmpegPath()
	if err != nil {
		return err
	}
	if err := backend.ValidateExecutable(ffmpeg); err != nil {
		return err
	}
	args := []string{"-hide_banner", "-nostats", "-i", src, "-map", "0:a", "-ar", "44100", "-y"}
	if format == "aac" {
		args = append(args, "-c:a", "aac", "-b:a", "256k", "-f", "ipod", dst)
	} else {
		args = append(args, "-sample_fmt", "s16p", "-c:a", "alac", "-f", "ipod", dst)
	}
	cmd := command(ffmpeg, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if len(text) > 400 {
			text = text[len(text)-400:]
		}
		return fmt.Errorf("ffmpeg: %w: %s", err, text)
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
