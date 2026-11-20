//go:build windows

package github

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"unsafe"
)

func trustedPath(path string) bool {
	st, e := os.Stat(path)
	if e != nil || !st.Mode().IsRegular() {
		return false
	}
	token, e := windows.OpenCurrentProcessToken()
	if e != nil {
		return false
	}
	defer token.Close()
	user, e := token.GetTokenUser()
	if e != nil {
		return false
	}
	uid := user.User.Sid.String()
	for p := path; ; p = filepath.Dir(p) {
		sd, e := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		if e != nil {
			return false
		}
		owner, _, e := sd.Owner()
		if e != nil || owner == nil {
			return false
		}
		ownerID := owner.String()
		if ownerID != uid && ownerID != "S-1-5-18" && ownerID != "S-1-5-32-544" && ownerID != "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464" {
			return false
		}
		acl, _, e := sd.DACL()
		if e != nil || acl == nil {
			return false
		}
		for i := uint32(0); i < uint32(acl.AceCount); i++ {
			var ace *windows.ACCESS_ALLOWED_ACE
			if windows.GetAce(acl, i, &ace) != nil || ace == nil {
				return false
			}
			if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
				continue
			}
			if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
				continue
			}
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
			write := windows.ACCESS_MASK(windows.GENERIC_WRITE | windows.GENERIC_ALL | windows.WRITE_DAC | windows.WRITE_OWNER | windows.DELETE | windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES)
			if ace.Mask&write != 0 && sid != uid && sid != "S-1-5-18" && sid != "S-1-5-32-544" && sid != ownerID {
				return false
			}
		}
		if filepath.Dir(p) == p {
			return true
		}
	}
}
