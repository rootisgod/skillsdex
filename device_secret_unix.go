//go:build !windows

package skillsdex

// Unix connection files rely on owner-only file permissions (0600).
func protectDeviceConnection(data []byte) ([]byte, error)   { return data, nil }
func unprotectDeviceConnection(data []byte) ([]byte, error) { return data, nil }
