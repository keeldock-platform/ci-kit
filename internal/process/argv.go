package process

import (
	"path/filepath"
	"strings"
)

// validateArgv rejects interpreters' dynamic command forms. Trusted committed
// script files remain ordinary argv; the catalogue validates their source paths.
func validateArgv(argv []string) error {
	if len(argv) == 0 || !filepath.IsAbs(argv[0]) {
		return ErrInvalid
	}
	for _, arg := range argv {
		if strings.ContainsRune(arg, 0) {
			return ErrInvalid
		}
	}
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(argv[0])), ".exe")
	switch name {
	case "cmd":
		return ErrInvalid
	case "sh", "bash", "zsh", "dash", "ksh", "fish":
		script := false
		for _, arg := range argv[1:] {
			if script {
				break
			}
			if !strings.HasPrefix(arg, "-") {
				script = true
				break
			}
			switch arg {
			case "--noprofile", "--norc", "-e", "-u", "-eu", "-ue", "--":
			default:
				return ErrInvalid
			}
		}
		if !script {
			return ErrInvalid
		}
	case "powershell", "pwsh":
		profile, file := false, false
		for i := 1; i < len(argv); i++ {
			switch strings.ToLower(argv[i]) {
			case "-noprofile":
				profile = true
			case "-noninteractive", "-nologo":
			case "-file":
				if i+1 >= len(argv) || argv[i+1] == "" {
					return ErrInvalid
				}
				file = true
			default:
				return ErrInvalid
			}
			if file {
				break
			}
		}
		if !profile || !file {
			return ErrInvalid
		}
	}
	ext := strings.ToLower(filepath.Ext(argv[0]))
	if ext == ".cmd" || ext == ".bat" {
		return ErrInvalid
	}
	return nil
}
