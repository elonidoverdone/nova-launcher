//go:build !windows

// Package secure: on non-Windows builds (tests only) secrets are stored as-is.
package secure

func Encrypt(plain string) (string, error) { return plain, nil }
func Decrypt(enc string) (string, error)   { return enc, nil }
