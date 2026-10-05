package adb

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultADBAddr = "127.0.0.1:5037"
	maxChunkSize   = 64 * 1024 // 64 KiB
)

// Device represents an Android device reported by the adb server.
type Device struct {
	Serial      string `json:"serial"`
	State       string `json:"state"` // "device", "unauthorized", "offline"
	Product     string `json:"product"`
	Model       string `json:"model"`
	Device      string `json:"device"`
	TransportID string `json:"transport_id"`
}

// DirEntry represents an entry on the remote filesystem returned by sync LIST or STAT.
type DirEntry struct {
	Name  string `json:"name"`
	Mode  uint32 `json:"mode"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
	IsDir bool   `json:"is_dir"`
	// IsSymlink is true when the entry itself is a link. Symlink targets
	// must never be followed, otherwise a phone-side link could read or
	// write files outside the managed music root.
	IsSymlink bool `json:"is_symlink"`
}

// Client talks to the local ADB server over TCP.
type Client struct {
	Addr string
}

// NewClient returns a client targeting addr (defaults to 127.0.0.1:5037).
func NewClient(addr string) *Client {
	if addr == "" {
		addr = defaultADBAddr
	}
	return &Client{Addr: addr}
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", c.Addr)
}

// watchConn closes conn as soon as ctx is cancelled, so a cancelled context
// interrupts a sync read or write that would otherwise block until the
// device answers. stop must be called when the caller is done.
func watchConn(ctx context.Context, conn net.Conn) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

func writeHexMsg(w io.Writer, msg string) error {
	prefix := fmt.Sprintf("%04x", len(msg))
	if _, err := io.WriteString(w, prefix+msg); err != nil {
		return err
	}
	return nil
}

func readStatus(r io.Reader) error {
	var status [4]byte
	if _, err := io.ReadFull(r, status[:]); err != nil {
		return err
	}
	s := string(status[:])
	if s == "OKAY" {
		return nil
	}
	if s == "FAIL" {
		var lenBuf [4]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			return errors.New("adb returned FAIL (unable to read error length)")
		}
		msgLen, err := strconv.ParseInt(string(lenBuf[:]), 16, 32)
		if err != nil {
			return errors.New("adb returned FAIL (invalid error length)")
		}
		msgBuf := make([]byte, msgLen)
		if _, err := io.ReadFull(r, msgBuf); err != nil {
			return errors.New("adb returned FAIL (unable to read error message)")
		}
		return fmt.Errorf("adb fail: %s", string(msgBuf))
	}
	return fmt.Errorf("adb unknown status: %s", s)
}

func readHexPayload(r io.Reader) (string, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return "", err
	}
	payloadLen, err := strconv.ParseInt(string(lenBuf[:]), 16, 32)
	if err != nil {
		return "", fmt.Errorf("invalid payload length %q: %w", string(lenBuf[:]), err)
	}
	buf := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// Version returns the internal ADB server protocol version (e.g. 41).
func (c *Client) Version(ctx context.Context) (int, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	if err := writeHexMsg(conn, "host:version"); err != nil {
		return 0, err
	}
	if err := readStatus(conn); err != nil {
		return 0, err
	}
	payload, err := readHexPayload(conn)
	if err != nil {
		return 0, err
	}
	ver, err := strconv.ParseInt(payload, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("parse adb version: %w", err)
	}
	return int(ver), nil
}

// Devices returns connected devices from host:devices-l.
func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := writeHexMsg(conn, "host:devices-l"); err != nil {
		return nil, err
	}
	if err := readStatus(conn); err != nil {
		return nil, err
	}
	payload, err := readHexPayload(conn)
	if err != nil {
		return nil, err
	}
	return ParseDevicesList(payload), nil
}

// ParseDevicesList parses lines from `host:devices-l`.
func ParseDevicesList(text string) []Device {
	var devices []Device
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		dev := Device{
			Serial: fields[0],
			State:  fields[1],
		}
		for _, field := range fields[2:] {
			if k, v, ok := strings.Cut(field, ":"); ok {
				switch k {
				case "product":
					dev.Product = v
				case "model":
					dev.Model = strings.ReplaceAll(v, "_", " ")
				case "device":
					dev.Device = v
				case "transport_id":
					dev.TransportID = v
				}
			}
		}
		if dev.Model == "" {
			dev.Model = dev.Product
		}
		if dev.Model == "" {
			dev.Model = dev.Device
		}
		devices = append(devices, dev)
	}
	return devices
}

func (c *Client) openTransport(ctx context.Context, serial string) (net.Conn, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	req := "host:transport:" + serial
	if err := writeHexMsg(conn, req); err != nil {
		conn.Close()
		return nil, err
	}
	if err := readStatus(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// Shell executes cmd on the device and returns the combined stdout and stderr.
func (c *Client) Shell(ctx context.Context, serial, cmd string) ([]byte, error) {
	conn, err := c.openTransport(ctx, serial)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := writeHexMsg(conn, "shell:"+cmd); err != nil {
		return nil, err
	}
	if err := readStatus(conn); err != nil {
		return nil, err
	}

	done := make(chan struct{})
	var out []byte
	var readErr error

	go func() {
		out, readErr = io.ReadAll(conn)
		close(done)
	}()

	select {
	case <-ctx.Done():
		conn.Close()
		<-done
		return nil, ctx.Err()
	case <-done:
		return out, readErr
	}
}

// openSync connects to the adbd sync service on serial.
func (c *Client) openSync(ctx context.Context, serial string) (net.Conn, error) {
	conn, err := c.openTransport(ctx, serial)
	if err != nil {
		return nil, err
	}
	if err := writeHexMsg(conn, "sync:"); err != nil {
		conn.Close()
		return nil, err
	}
	if err := readStatus(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// Stat requests the file mode, size, and mtime for remotePath via sync STAT.
func (c *Client) Stat(ctx context.Context, serial, remotePath string) (*DirEntry, error) {
	conn, err := c.openSync(ctx, serial)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	defer watchConn(ctx, conn)()

	// Write STAT request: "STAT" (4B) + uint32(len) + path
	pathBytes := []byte(remotePath)
	req := make([]byte, 8+len(pathBytes))
	copy(req[0:4], "STAT")
	binary.LittleEndian.PutUint32(req[4:8], uint32(len(pathBytes)))
	copy(req[8:], pathBytes)

	if _, err := conn.Write(req); err != nil {
		return nil, err
	}

	var resp [16]byte // "STAT" (4B) + mode (4B) + size (4B) + mtime (4B)
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		return nil, err
	}
	if string(resp[0:4]) != "STAT" {
		return nil, fmt.Errorf("unexpected stat response header: %q", resp[0:4])
	}
	mode := binary.LittleEndian.Uint32(resp[4:8])
	size := binary.LittleEndian.Uint32(resp[8:12])
	mtime := binary.LittleEndian.Uint32(resp[12:16])

	// S_IFDIR is 0040000 = 0x4000; S_IFMT masks the file-type bits.
	isDir := (mode & 0o040000) != 0
	isSymlink := (mode & 0o170000) == 0o120000

	return &DirEntry{
		Name:      remotePath,
		Mode:      mode,
		Size:      int64(size),
		Mtime:     int64(mtime),
		IsDir:     isDir,
		IsSymlink: isSymlink,
	}, nil
}

// List lists entries in dirPath via sync LIST.
func (c *Client) List(ctx context.Context, serial, dirPath string) ([]DirEntry, error) {
	conn, err := c.openSync(ctx, serial)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	defer watchConn(ctx, conn)()

	pathBytes := []byte(dirPath)
	req := make([]byte, 8+len(pathBytes))
	copy(req[0:4], "LIST")
	binary.LittleEndian.PutUint32(req[4:8], uint32(len(pathBytes)))
	copy(req[8:], pathBytes)

	if _, err := conn.Write(req); err != nil {
		return nil, err
	}

	var entries []DirEntry
	var hdr [4]byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := io.ReadFull(conn, hdr[:]); err != nil {
			return nil, err
		}
		tag := string(hdr[:])
		if tag == "DONE" {
			var zero [16]byte
			_, _ = io.ReadFull(conn, zero[:])
			break
		}
		if tag != "DENT" {
			return nil, fmt.Errorf("unexpected list response tag: %q", tag)
		}
		var fields [16]byte // mode(4B) + size(4B) + mtime(4B) + namelen(4B)
		if _, err := io.ReadFull(conn, fields[:]); err != nil {
			return nil, err
		}
		mode := binary.LittleEndian.Uint32(fields[0:4])
		size := binary.LittleEndian.Uint32(fields[4:8])
		mtime := binary.LittleEndian.Uint32(fields[8:12])
		namelen := binary.LittleEndian.Uint32(fields[12:16])

		nameBuf := make([]byte, namelen)
		if _, err := io.ReadFull(conn, nameBuf); err != nil {
			return nil, err
		}
		name := string(nameBuf)
		if name == "." || name == ".." {
			continue
		}
		entries = append(entries, DirEntry{
			Name:      name,
			Mode:      mode,
			Size:      int64(size),
			Mtime:     int64(mtime),
			IsDir:     (mode & 0o040000) != 0,
			IsSymlink: (mode & 0o170000) == 0o120000,
		})
	}

	// Send QUIT
	_ = writeQuit(conn)
	return entries, nil
}

// Send streams a local file to remotePath on the device using sync SEND.
func (c *Client) Send(ctx context.Context, serial, localPath, remotePath string, mode uint32, mtime time.Time, progress func(int64)) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	conn, err := c.openSync(ctx, serial)
	if err != nil {
		return err
	}
	defer conn.Close()
	defer watchConn(ctx, conn)()

	if mode == 0 {
		mode = 0o644
	}
	param := fmt.Sprintf("%s,%d", remotePath, mode)
	paramBytes := []byte(param)

	req := make([]byte, 8+len(paramBytes))
	copy(req[0:4], "SEND")
	binary.LittleEndian.PutUint32(req[4:8], uint32(len(paramBytes)))
	copy(req[8:], paramBytes)

	if _, err := conn.Write(req); err != nil {
		return err
	}

	buf := make([]byte, maxChunkSize)
	var chunkHdr [8]byte
	copy(chunkHdr[0:4], "DATA")
	var totalSent int64

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := f.Read(buf)
		if n > 0 {
			binary.LittleEndian.PutUint32(chunkHdr[4:8], uint32(n))
			if _, err := conn.Write(chunkHdr[:]); err != nil {
				return err
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return err
			}
			totalSent += int64(n)
			if progress != nil {
				progress(totalSent)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}

	// Send DONE packet: "DONE" (4B) + uint32(mtime)
	var donePkt [8]byte
	copy(donePkt[0:4], "DONE")
	binary.LittleEndian.PutUint32(donePkt[4:8], uint32(mtime.Unix()))
	if _, err := conn.Write(donePkt[:]); err != nil {
		return err
	}

	// Read response (OKAY or FAIL)
	var status [4]byte
	if _, err := io.ReadFull(conn, status[:]); err != nil {
		return err
	}
	if string(status[:]) != "OKAY" {
		var lenBuf [4]byte
		if _, err := io.ReadFull(conn, lenBuf[:]); err == nil {
			msgLen := binary.LittleEndian.Uint32(lenBuf[:])
			msgBuf := make([]byte, msgLen)
			_, _ = io.ReadFull(conn, msgBuf)
			return fmt.Errorf("sync send failed: %s", string(msgBuf))
		}
		return fmt.Errorf("sync send returned status %q", status[:])
	}

	_ = writeQuit(conn)
	return nil
}

// Recv streams remotePath to w via sync RECV.
func (c *Client) Recv(ctx context.Context, serial, remotePath string, w io.Writer) error {
	conn, err := c.openSync(ctx, serial)
	if err != nil {
		return err
	}
	defer conn.Close()
	defer watchConn(ctx, conn)()

	pathBytes := []byte(remotePath)
	req := make([]byte, 8+len(pathBytes))
	copy(req[0:4], "RECV")
	binary.LittleEndian.PutUint32(req[4:8], uint32(len(pathBytes)))
	copy(req[8:], pathBytes)

	if _, err := conn.Write(req); err != nil {
		return err
	}

	var hdr [4]byte
	var lenBuf [4]byte
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := io.ReadFull(conn, hdr[:]); err != nil {
			return err
		}
		tag := string(hdr[:])
		if tag == "DONE" {
			_, _ = io.ReadFull(conn, lenBuf[:])
			break
		}
		if tag == "FAIL" {
			if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
				return errors.New("sync recv failed")
			}
			msgLen := binary.LittleEndian.Uint32(lenBuf[:])
			msgBuf := make([]byte, msgLen)
			_, _ = io.ReadFull(conn, msgBuf)
			return fmt.Errorf("sync recv failed: %s", string(msgBuf))
		}
		if tag != "DATA" {
			return fmt.Errorf("unexpected sync recv tag: %q", tag)
		}
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return err
		}
		chunkLen := binary.LittleEndian.Uint32(lenBuf[:])
		chunkBuf := make([]byte, chunkLen)
		if _, err := io.ReadFull(conn, chunkBuf); err != nil {
			return err
		}
		if _, err := w.Write(chunkBuf); err != nil {
			return err
		}
	}

	_ = writeQuit(conn)
	return nil
}

func writeQuit(conn net.Conn) error {
	var quit [8]byte
	copy(quit[0:4], "QUIT")
	_, err := conn.Write(quit[:])
	return err
}
