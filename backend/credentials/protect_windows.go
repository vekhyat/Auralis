//go:build windows

package credentials

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// protect encrypts data with Windows DPAPI (CryptProtectData), scoped to the
// current Windows user. The same user on the same machine is required to
// decrypt; nothing is written in plaintext.
func protect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("credentials: empty secret")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	entropy, _ := windows.UTF16PtrFromString("Auralis credentials v1")
	entropyBlob := windows.DataBlob{Size: uint32(len("Auralis credentials v1")) * 2, Data: (*byte)(unsafe.Pointer(entropy))}
	err := windows.CryptProtectData(&in, nil, &entropyBlob, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	blob := make([]byte, out.Size)
	copy(blob, unsafe.Slice(out.Data, out.Size))
	return blob, nil
}

func unprotect(blob []byte) ([]byte, error) {
	if len(blob) == 0 {
		return nil, errors.New("credentials: empty blob")
	}
	in := windows.DataBlob{Size: uint32(len(blob)), Data: &blob[0]}
	var out windows.DataBlob
	entropy, _ := windows.UTF16PtrFromString("Auralis credentials v1")
	entropyBlob := windows.DataBlob{Size: uint32(len("Auralis credentials v1")) * 2, Data: (*byte)(unsafe.Pointer(entropy))}
	err := windows.CryptUnprotectData(&in, nil, &entropyBlob, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	plain := make([]byte, out.Size)
	copy(plain, unsafe.Slice(out.Data, out.Size))
	return plain, nil
}
