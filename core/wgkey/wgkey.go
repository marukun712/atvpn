package wgkey

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func GeneratePrivateKey() (wgtypes.Key, error) {
	return wgtypes.GeneratePrivateKey()
}

func Fingerprint(pub wgtypes.Key) string {
	sum := sha256.Sum256(pub[:])
	digits := strings.ToUpper(hex.EncodeToString(sum[:4]))
	return digits[:4] + "-" + digits[4:]
}
