//go:build windows

package skillsdex

import (
	"bytes"
	"errors"
	"golang.org/x/sys/windows"
	"unsafe"
)

var deviceSecretPrefix = []byte("SkillsDex-DPAPI-v1\n")

func protectDeviceConnection(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty device connection")
	}
	input := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var output windows.DataBlob
	if err := windows.CryptProtectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	protected := append([]byte{}, deviceSecretPrefix...)
	return append(protected, unsafe.Slice(output.Data, int(output.Size))...), nil
}

func unprotectDeviceConnection(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, deviceSecretPrefix) {
		return nil, errors.New("device connection is not protected by Windows DPAPI; reconnect this machine")
	}
	data = data[len(deviceSecretPrefix):]
	if len(data) == 0 {
		return nil, errors.New("empty protected device connection")
	}
	input := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var output windows.DataBlob
	if err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	return append([]byte{}, unsafe.Slice(output.Data, int(output.Size))...), nil
}
