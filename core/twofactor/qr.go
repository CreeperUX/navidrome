package twofactor

import (
	"encoding/base64"

	"rsc.io/qr"
)

// QRCode is the otpauth:// link as a QR code for authenticator apps to scan: a PNG in a data: URL.
func QRCode(uri string) (string, error) {
	code, err := qr.Encode(uri, qr.M)
	if err != nil {
		return "", err
	}
	code.Scale = 6
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()), nil
}
