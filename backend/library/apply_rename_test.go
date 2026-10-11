package library

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writePlain(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readPlain(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func stampedMove(t *testing.T, from, to string) Operation {
	t.Helper()
	info, err := os.Stat(from)
	if err != nil {
		t.Fatal(err)
	}
	return Operation{Type: "move", Path: from, NewPath: to, Size: info.Size(), ModTime: info.ModTime().UnixNano()}
}

func renameTemps(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".auralis-rename-") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestSwapRenameAppliesAndUndo(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "A.flac")
	b := filepath.Join(root, "B.flac")
	writePlain(t, a, "file-a")
	writePlain(t, b, "file-b")
	writePlain(t, strings.TrimSuffix(a, ".flac")+".lrc", "lyric-a")
	writePlain(t, strings.TrimSuffix(b, ".flac")+".lrc", "lyric-b")
	plan := &Plan{Operations: []Operation{stampedMove(t, a, b), stampedMove(t, b, a)}}
	journal := filepath.Join(t.TempDir(), "j.jsonl")

	var seen []ApplyProgress
	result, err := ApplyWithProgress(context.Background(), root, journal, plan, func(p ApplyProgress) {
		if p.Current > p.Total || p.Total < 1 {
			t.Errorf("progress %+v", p)
		}
		seen = append(seen, p)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 3 || len(result.Errors) != 0 {
		t.Fatalf("swap apply: %+v", result)
	}
	if len(seen) != 3 || seen[len(seen)-1].Current != seen[len(seen)-1].Total {
		t.Fatalf("progress: %+v", seen)
	}
	if readPlain(t, a) != "file-b" || readPlain(t, b) != "file-a" {
		t.Fatalf("files did not swap: A=%q B=%q", readPlain(t, a), readPlain(t, b))
	}
	if readPlain(t, strings.TrimSuffix(a, ".flac")+".lrc") != "lyric-b" || readPlain(t, strings.TrimSuffix(b, ".flac")+".lrc") != "lyric-a" {
		t.Fatal("lyrics did not follow the swapped files")
	}
	if temps := renameTemps(t, root); len(temps) != 0 {
		t.Fatalf("temporary rename left behind: %v", temps)
	}

	undo, err := UndoLast(journal)
	if err != nil || undo.Applied != 3 || len(undo.Errors) != 0 {
		t.Fatalf("undo: %+v %v", undo, err)
	}
	if readPlain(t, a) != "file-a" || readPlain(t, b) != "file-b" {
		t.Fatalf("undo files: A=%q B=%q", readPlain(t, a), readPlain(t, b))
	}
	if readPlain(t, strings.TrimSuffix(a, ".flac")+".lrc") != "lyric-a" || readPlain(t, strings.TrimSuffix(b, ".flac")+".lrc") != "lyric-b" {
		t.Fatal("undo did not restore lyrics")
	}
}

func TestRenameCycleOfThree(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "A.txt")
	b := filepath.Join(root, "B.txt")
	c := filepath.Join(root, "C.txt")
	writePlain(t, a, "A")
	writePlain(t, b, "B")
	writePlain(t, c, "C")
	plan := &Plan{Operations: []Operation{
		stampedMove(t, a, b),
		stampedMove(t, b, c),
		stampedMove(t, c, a),
	}}
	result, err := Apply(context.Background(), root, filepath.Join(t.TempDir(), "j.jsonl"), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 4 || len(result.Errors) != 0 {
		t.Fatalf("cycle: %+v", result)
	}
	if readPlain(t, a) != "C" || readPlain(t, b) != "A" || readPlain(t, c) != "B" {
		t.Fatalf("cycle contents A=%q B=%q C=%q", readPlain(t, a), readPlain(t, b), readPlain(t, c))
	}
	if temps := renameTemps(t, root); len(temps) != 0 {
		t.Fatalf("temporary rename left behind: %v", temps)
	}
}

func TestChainedRenameAcrossDirectories(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a", "song.txt")
	b := filepath.Join(root, "b", "song.txt")
	c := filepath.Join(root, "c", "song.txt")
	writePlain(t, a, "from-a")
	writePlain(t, b, "from-b")
	plan := &Plan{Operations: []Operation{stampedMove(t, a, b), stampedMove(t, b, c)}}
	result, err := Apply(context.Background(), root, filepath.Join(t.TempDir(), "j.jsonl"), plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("cross-dir chain: %+v", result)
	}
	if readPlain(t, b) != "from-a" || readPlain(t, c) != "from-b" {
		t.Fatalf("b=%q c=%q", readPlain(t, b), readPlain(t, c))
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatalf("source a should be gone, stat err=%v", err)
	}
	if temps := renameTemps(t, root); len(temps) != 0 {
		t.Fatalf("temporary rename left behind: %v", temps)
	}
}

func TestMoveOntoUnrelatedFileIsNotOverwritten(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "A.txt")
	b := filepath.Join(root, "B.txt")
	writePlain(t, a, "file-a")
	writePlain(t, b, "keep-b")
	result, err := Apply(context.Background(), root, filepath.Join(t.TempDir(), "j.jsonl"), &Plan{
		Operations: []Operation{stampedMove(t, a, b)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 0 || result.Skipped != 1 {
		t.Fatalf("result: %+v", result)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "destination already exists") {
		t.Fatalf("errors: %+v", result.Errors)
	}
	if readPlain(t, a) != "file-a" || readPlain(t, b) != "keep-b" {
		t.Fatal("an occupied destination was overwritten")
	}
}

func TestCaseInsensitiveSwap(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("case-insensitive volumes only")
	}
	root := t.TempDir()
	a := filepath.Join(root, "A.txt")
	b := filepath.Join(root, "B.txt")
	writePlain(t, a, "file-a")
	writePlain(t, b, "file-b")
	// Destinations differ only by case from the other source. On a
	// case-insensitive volume that is still the same file.
	plan := &Plan{Operations: []Operation{
		stampedMove(t, a, filepath.Join(root, "b.txt")),
		stampedMove(t, b, filepath.Join(root, "a.txt")),
	}}
	result, err := Apply(context.Background(), root, filepath.Join(t.TempDir(), "j.jsonl"), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 3 || len(result.Errors) != 0 {
		t.Fatalf("case swap: %+v", result)
	}
	if readPlain(t, a) != "file-b" || readPlain(t, b) != "file-a" {
		t.Fatalf("A=%q B=%q", readPlain(t, a), readPlain(t, b))
	}
}
