package ipod

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vekhyat/Auralis/backend"
)

// copyYield runs between copy reads. Tests cancel from it.
var copyYield = func() {}

// beforeFFmpegWait runs after the FFmpeg command is bound to ctx and before
// it starts. Tests cancel from it.
var beforeFFmpegWait = func() {}

func copyFile(src, dst string) error {
	return copyFileCtx(context.Background(), src, dst)
}

func copyFileCtx(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	copyErr := copyCtx(ctx, out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("copy: %w", firstErr(copyErr, syncErr, closeErr))
	}
	return nil
}

func copyCtx(ctx context.Context, dst io.Writer, src io.Reader) error {
	buf := make([]byte, 32*1024)
	for {
		copyYield()
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func transcodeFile(src, dst, format string) error {
	return transcodeFileCtx(context.Background(), src, dst, format)
}

func transcodeFileCtx(ctx context.Context, src, dst, format string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
	cmd := commandContext(ctx, ffmpeg, args...)
	beforeFFmpegWait()
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(dst)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		text := strings.TrimSpace(string(out))
		if len(text) > 400 {
			text = text[len(text)-400:]
		}
		return fmt.Errorf("ffmpeg: %w: %s", err, text)
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

func hashSource(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	buf := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, rerr := f.Read(buf)
		if n > 0 {
			if _, werr := sum.Write(buf[:n]); werr != nil {
				return "", werr
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
