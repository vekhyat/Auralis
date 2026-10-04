package adb

import (
	"regexp"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type fakeFile struct {
	mode  uint32
	size  int64
	mtime int64
	data  []byte
}

type fakeServer struct {
	listener net.Listener
	mu       sync.Mutex
	files    map[string]*fakeFile
	dirs     map[string][]string // dir -> list of filenames
	commands []string
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}

	fs := &fakeServer{
		listener: ln,
		files:    make(map[string]*fakeFile),
		dirs:     make(map[string][]string),
	}

	// Default files in fake phone
	fs.files["/sdcard/Music/Track1.mp3"] = &fakeFile{
		mode:  0o100644,
		size:  1024,
		mtime: 1700000000,
		data:  bytes.Repeat([]byte{0x42}, 1024),
	}
	fs.dirs["/sdcard/Music"] = []string{"Track1.mp3"}

	go fs.serve()
	return fs
}

func (s *fakeServer) addr() string {
	return s.listener.Addr().String()
}

func (s *fakeServer) close() {
	_ = s.listener.Close()
}

func (s *fakeServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *fakeServer) handleConn(conn net.Conn) {
	defer conn.Close()

	for {
		// Read 4 hex bytes length
		var lenBuf [4]byte
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return
		}
		reqLen, err := strconv.ParseInt(string(lenBuf[:]), 16, 32)
		if err != nil {
			return
		}
		reqBuf := make([]byte, reqLen)
		if _, err := io.ReadFull(conn, reqBuf); err != nil {
			return
		}
		req := string(reqBuf)

		switch {
		case req == "host:version":
			_, _ = io.WriteString(conn, "OKAY00040029")
			return
		case req == "host:devices-l":
			payload := "emulator-5554 device product:sdk_gphone64_x86_64 model:Pixel_7 device:emu64xa transport_id:1\n"
			resp := fmt.Sprintf("OKAY%04x%s", len(payload), payload)
			_, _ = io.WriteString(conn, resp)
			return
		case strings.HasPrefix(req, "host:transport:"):
			_, _ = io.WriteString(conn, "OKAY")
			// Connection is now switched to device transport
			s.handleDeviceTransport(conn)
			return
		default:
			errMsg := "unknown host service"
			resp := fmt.Sprintf("FAIL%04x%s", len(errMsg), errMsg)
			_, _ = io.WriteString(conn, resp)
			return
		}
	}
}

func (s *fakeServer) handleDeviceTransport(conn net.Conn) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return
	}
	reqLen, err := strconv.ParseInt(string(lenBuf[:]), 16, 32)
	if err != nil {
		return
	}
	reqBuf := make([]byte, reqLen)
	if _, err := io.ReadFull(conn, reqBuf); err != nil {
		return
	}
	req := string(reqBuf)

	switch {
	case strings.HasPrefix(req, "shell:"):
		cmd := strings.TrimPrefix(req, "shell:")
		s.mu.Lock()
		s.commands = append(s.commands, cmd)
		s.mu.Unlock()
		_, _ = io.WriteString(conn, "OKAY")
		s.handleShellCommand(conn, cmd)
		return
	case req == "sync:":
		_, _ = io.WriteString(conn, "OKAY")
		s.handleSyncService(conn)
		return
	default:
		_, _ = io.WriteString(conn, "FAIL000eunsupported req")
		return
	}
}

var fakeMvPattern = regexp.MustCompile(`mv (?:-f )?'([^']*)' '([^']*)'`)

func (s *fakeServer) handleShellCommand(conn net.Conn, cmd string) {
	// Commands wrapped by Target.run expect an exit-status trailer. Paths
	// under /readonly fail, like a write to a protected folder would.
	if strings.HasSuffix(cmd, "echo __AURALIS_RC=$?") {
		s.mu.Lock()
		s.commands = append(s.commands, cmd)
		// Emulate "mv -f 'from' 'to'" so uploads land under their final name.
		if m := fakeMvPattern.FindStringSubmatch(cmd); m != nil && !strings.Contains(cmd, "/readonly/") {
			if f, ok := s.files[m[1]]; ok {
				s.files[m[2]] = f
				delete(s.files, m[1])
			}
		}
		s.mu.Unlock()
		if strings.Contains(cmd, "/readonly/") {
			_, _ = io.WriteString(conn, "mv: Permission denied\r\n__AURALIS_RC=1\r\n")
			return
		}
		_, _ = io.WriteString(conn, "__AURALIS_RC=0\r\n")
		return
	}
	if strings.HasPrefix(cmd, "df") {
		// Output toybox df format
		out := "Filesystem     1K-blocks      Used Available Use% Mounted on\n" +
			"/data/media     59281728  20112456  39044944  34% /storage/emulated\n"
		_, _ = io.WriteString(conn, out)
		return
	}
	if strings.HasPrefix(cmd, "am broadcast") {
		_, _ = io.WriteString(conn, "Broadcasting: Intent { act=android.intent.action.MEDIA_SCANNER_SCAN_FILE }\nBroadcast completed: result=0\n")
		return
	}
	if strings.Contains(cmd, "mv ") {
		// Mock move
		_, _ = io.WriteString(conn, "")
		return
	}
	if strings.Contains(cmd, "mkdir -p ") {
		_, _ = io.WriteString(conn, "")
		return
	}
	if strings.Contains(cmd, "rm -f ") {
		_, _ = io.WriteString(conn, "")
		return
	}
	_, _ = io.WriteString(conn, "")
}

func (s *fakeServer) handleSyncService(conn net.Conn) {
	for {
		var idBuf [4]byte
		if _, err := io.ReadFull(conn, idBuf[:]); err != nil {
			return
		}
		id := string(idBuf[:])

		switch id {
		case "QUIT":
			var zero [4]byte
			_, _ = io.ReadFull(conn, zero[:])
			return
		case "STAT":
			var lenBuf [4]byte
			if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
				return
			}
			pathLen := binary.LittleEndian.Uint32(lenBuf[:])
			pBuf := make([]byte, pathLen)
			if _, err := io.ReadFull(conn, pBuf); err != nil {
				return
			}
			filePath := string(pBuf)

			s.mu.Lock()
			f, ok := s.files[filePath]
			s.mu.Unlock()

			var resp [16]byte
			copy(resp[0:4], "STAT")
			if ok {
				binary.LittleEndian.PutUint32(resp[4:8], f.mode)
				binary.LittleEndian.PutUint32(resp[8:12], uint32(f.size))
				binary.LittleEndian.PutUint32(resp[12:16], uint32(f.mtime))
			}
			_, _ = conn.Write(resp[:])

		case "LIST":
			var lenBuf [4]byte
			if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
				return
			}
			pathLen := binary.LittleEndian.Uint32(lenBuf[:])
			pBuf := make([]byte, pathLen)
			if _, err := io.ReadFull(conn, pBuf); err != nil {
				return
			}
			dirPath := string(pBuf)

			s.mu.Lock()
			names := s.dirs[dirPath]
			s.mu.Unlock()

			for _, name := range names {
				full := dirPath + "/" + name
				s.mu.Lock()
				f := s.files[full]
				s.mu.Unlock()

				var dent [20]byte
				copy(dent[0:4], "DENT")
				mode := uint32(0o100644)
				size := uint32(100)
				mtime := uint32(1700000000)
				if f != nil {
					mode = f.mode
					size = uint32(f.size)
					mtime = uint32(f.mtime)
				}
				binary.LittleEndian.PutUint32(dent[4:8], mode)
				binary.LittleEndian.PutUint32(dent[8:12], size)
				binary.LittleEndian.PutUint32(dent[12:16], mtime)
				binary.LittleEndian.PutUint32(dent[16:20], uint32(len(name)))

				_, _ = conn.Write(dent[:])
				_, _ = io.WriteString(conn, name)
			}

			// Send DONE
			var done [20]byte
			copy(done[0:4], "DONE")
			_, _ = conn.Write(done[:])

		case "SEND":
			var lenBuf [4]byte
			if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
				return
			}
			paramLen := binary.LittleEndian.Uint32(lenBuf[:])
			paramBuf := make([]byte, paramLen)
			if _, err := io.ReadFull(conn, paramBuf); err != nil {
				return
			}
			param := string(paramBuf)
			remotePath := strings.Split(param, ",")[0]

			var fileData bytes.Buffer
			var mtime uint32

			// Read DATA chunks until DONE
			for {
				var chunkId [4]byte
				if _, err := io.ReadFull(conn, chunkId[:]); err != nil {
					return
				}
				tag := string(chunkId[:])
				if tag == "DONE" {
					var mtimeBuf [4]byte
					_, _ = io.ReadFull(conn, mtimeBuf[:])
					mtime = binary.LittleEndian.Uint32(mtimeBuf[:])
					break
				}
				if tag != "DATA" {
					return
				}
				var cLenBuf [4]byte
				if _, err := io.ReadFull(conn, cLenBuf[:]); err != nil {
					return
				}
				cLen := binary.LittleEndian.Uint32(cLenBuf[:])
				cBuf := make([]byte, cLen)
				if _, err := io.ReadFull(conn, cBuf); err != nil {
					return
				}
				fileData.Write(cBuf)
			}

			s.mu.Lock()
			s.files[remotePath] = &fakeFile{
				mode:  0o100644,
				size:  int64(fileData.Len()),
				mtime: int64(mtime),
				data:  fileData.Bytes(),
			}
			s.mu.Unlock()

			_, _ = io.WriteString(conn, "OKAY")

		case "RECV":
			var lenBuf [4]byte
			if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
				return
			}
			pathLen := binary.LittleEndian.Uint32(lenBuf[:])
			pBuf := make([]byte, pathLen)
			if _, err := io.ReadFull(conn, pBuf); err != nil {
				return
			}
			filePath := string(pBuf)

			s.mu.Lock()
			f, ok := s.files[filePath]
			s.mu.Unlock()

			if !ok {
				errMsg := "file not found"
				var failPkt [8]byte
				copy(failPkt[0:4], "FAIL")
				binary.LittleEndian.PutUint32(failPkt[4:8], uint32(len(errMsg)))
				_, _ = conn.Write(failPkt[:])
				_, _ = io.WriteString(conn, errMsg)
				return
			}

			// Send DATA chunk
			var dataPkt [8]byte
			copy(dataPkt[0:4], "DATA")
			binary.LittleEndian.PutUint32(dataPkt[4:8], uint32(len(f.data)))
			_, _ = conn.Write(dataPkt[:])
			_, _ = conn.Write(f.data)

			// Send DONE
			var donePkt [8]byte
			copy(donePkt[0:4], "DONE")
			_, _ = conn.Write(donePkt[:])
		}
	}
}
