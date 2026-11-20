package github

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"regexp"
	"strings"
)

type signerRole struct {
	Person, Lane bool
	Principals   map[string]bool
}

var fingerprintRE = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)

// signerRoles interprets the admitted OpenSSH Ed25519 policy used by the existing hooks.
// Signature parsing and verification remain Git/OpenSSH responsibilities.
func signerRoles(data []byte) (map[string]signerRole, error) {
	roles := map[string]signerRole{}
	if len(data) > 1<<20 {
		return nil, Fail(3, "policy_refused")
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		position := -1
		for _, i := range []int{1, 2} {
			if i < len(fields) && fields[i] == "ssh-ed25519" {
				if position != -1 {
					return nil, Fail(3, "policy_refused")
				}
				position = i
			}
		}
		if position < 0 || position+1 >= len(fields) {
			return nil, Fail(3, "policy_refused")
		}
		blob, e := base64.StdEncoding.Strict().DecodeString(fields[position+1])
		if e != nil || len(blob) != 51 || binary.BigEndian.Uint32(blob[:4]) != 11 || !bytes.Equal(blob[4:15], []byte("ssh-ed25519")) || binary.BigEndian.Uint32(blob[15:19]) != 32 {
			return nil, Fail(3, "policy_refused")
		}
		sum := sha256.Sum256(blob)
		fp := "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
		role := roles[fp]
		if role.Principals == nil {
			role.Principals = map[string]bool{}
		}
		for _, principal := range strings.Split(fields[0], ",") {
			if principal == "" || strings.ContainsAny(principal, "*?! \t\r\n") {
				return nil, Fail(3, "policy_refused")
			}
			role.Principals[principal] = true
			if strings.HasPrefix(strings.ToLower(principal), "lane-") {
				role.Lane = true
			} else {
				role.Person = true
			}
		}
		roles[fp] = role
	}
	if len(roles) == 0 {
		return nil, Fail(3, "policy_refused")
	}
	return roles, nil
}
func revokedFingerprints(data []byte) (map[string]bool, error) {
	out := map[string]bool{}
	if len(data) == 0 {
		return out, nil
	}
	if len(data) > 64<<10 || data[len(data)-1] != '\n' || bytes.ContainsRune(data, '\r') {
		return nil, Fail(3, "policy_refused")
	}
	for _, line := range strings.Split(string(data[:len(data)-1]), "\n") {
		if !fingerprintRE.MatchString(line) || out[line] {
			return nil, Fail(3, "policy_refused")
		}
		out[line] = true
	}
	return out, nil
}
func sshSignatureObject(raw []byte) bool {
	header, _, ok := strings.Cut(string(raw), "\n\n")
	if !ok {
		return false
	}
	if strings.Count(header, "\ngpgsig ") != 1 {
		return false
	}
	return strings.Contains(header, "\ngpgsig -----BEGIN SSH SIGNATURE-----\n") && strings.Contains(header, "\n -----END SSH SIGNATURE-----") && !strings.Contains(header, "-----BEGIN PGP SIGNATURE-----")
}
