//go:build windows

package github

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"unsafe"
)

func trustedPath(path string) bool { ok, _ := trustedPathAssessment(path); return ok }
func trustedPathAssessment(path string) (bool, int) {
	phase := 1
	st, e := os.Stat(path)
	if e != nil || !st.Mode().IsRegular() {
		return false, phase
	}
	phase = 2
	token, e := windows.OpenCurrentProcessToken()
	if e != nil {
		return false, phase
	}
	defer token.Close()
	phase = 3
	user, e := token.GetTokenUser()
	if e != nil {
		return false, phase
	}
	uid := user.User.Sid.String()
	depth := 0
	for p := path; ; p = filepath.Dir(p) {
		depth++
		phase = 100 + depth*10 + 1
		sd, e := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		if e != nil {
			return false, phase
		}
		phase = 100 + depth*10 + 2
		owner, _, e := sd.Owner()
		if e != nil || owner == nil {
			return false, phase
		}
		phase = 100 + depth*10 + 3
		ownerID := owner.String()
		if ownerID != uid && ownerID != "S-1-5-18" && ownerID != "S-1-5-32-544" && ownerID != "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464" {
			return false, phase
		}
		phase = 100 + depth*10 + 4
		acl, _, e := sd.DACL()
		if e != nil || acl == nil {
			return false, phase
		}
		for i := uint32(0); i < uint32(acl.AceCount); i++ {
			phase = 100 + depth*10 + 5
			var ace *windows.ACCESS_ALLOWED_ACE
			if windows.GetAce(acl, i, &ace) != nil || ace == nil {
				return false, phase
			}
			if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
				continue
			}
			if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
				continue
			}
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
			info, statErr := os.Stat(p)
			if statErr != nil {
				return false, phase
			}
			write := pathMutationRights(info.IsDir())
			phase = 100 + depth*10 + 6
			if uint32(ace.Mask)&write != 0 && sid != uid && sid != "S-1-5-18" && sid != "S-1-5-32-544" && sid != ownerID {
				return false, phase
			}
		}
		if filepath.Dir(p) == p {
			return true, 0
		}
	}
}
