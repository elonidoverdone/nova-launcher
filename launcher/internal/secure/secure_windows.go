//go:build windows

// Package secure encrypts secrets for the current Windows user (DPAPI).
package secure

import (
	"encoding/base64"
	"unsafe"

	"golang.org/x/sys/windows"
)

func Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	in := []byte(plain)
	inBlob := windows.DataBlob{Size: uint32(len(in)), Data: &in[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&inBlob, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return "dpapi:" + base64.StdEncoding.EncodeToString(unsafe.Slice(out.Data, out.Size)), nil
}

func Decrypt(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	if len(enc) < 6 || enc[:6] != "dpapi:" {
		return enc, nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc[6:])
	if err != nil || len(raw) == 0 {
		return "", err
	}
	inBlob := windows.DataBlob{Size: uint32(len(raw)), Data: &raw[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&inBlob, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(unsafe.Slice(out.Data, out.Size)), nil
}
