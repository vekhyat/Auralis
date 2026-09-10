package backend

import (
	"crypto/cipher"
	"fmt"
	"strings"

	"github.com/vekhyat/Auralis/backend/blowfish"
)

const deezerEncryptedChunk = 2048

func decryptDeezerStream(encrypted []byte, key []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, fmt.Errorf("empty Deezer blowfish key")
	}
	block, err := blowfish.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("blowfish: %w", err)
	}
	iv := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	out := make([]byte, 0, len(encrypted))
	blockNum := 0
	for i := 0; i < len(encrypted); i += deezerEncryptedChunk {
		end := i + deezerEncryptedChunk
		if end > len(encrypted) {
			end = len(encrypted)
		}
		chunk := encrypted[i:end]
		if blockNum%3 == 0 && len(chunk) == deezerEncryptedChunk {
			chunk = append([]byte(nil), chunk...)
			mode := cipher.NewCBCDecrypter(block, iv)
			mode.CryptBlocks(chunk, chunk)
		}
		out = append(out, chunk...)
		blockNum++
	}
	return out, nil
}

func deezerBlowfishKey(raw string) []byte {
	key := []byte(strings.TrimSpace(raw))
	if len(key) > 56 {
		key = key[:56]
	}
	return key
}
