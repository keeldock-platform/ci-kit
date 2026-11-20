package acquire

import (
	"bufio"
	"io"
	"strings"
)

// ServeCredential implements Git's fixed credential protocol. The executable
// entrypoint calls it only for the "get" operation; store/erase never persist.
func ServeCredential(in io.Reader, out io.Writer, getenv func(string) string) error {
	scanner := bufio.NewScanner(io.LimitReader(in, 8193))
	scanner.Buffer(make([]byte, 1024), 8192)
	fields := map[string]string{}
	seen := map[string]bool{}
	size := 0
	for scanner.Scan() {
		line := scanner.Text()
		size += len(line) + 1
		if size > 8192 {
			return ErrRefused
		}
		if line == "" {
			break
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return ErrRefused
		}
		// Git repeats capability and authentication metadata arrays. They carry no
		// authority here; host, protocol and path must still occur exactly once.
		if k == "capability[]" || k == "wwwauth[]" || k == "state[]" {
			continue
		}
		if seen[k] {
			return ErrRefused
		}
		fields[k] = v
		seen[k] = true
	}
	if scanner.Err() != nil {
		return ErrRefused
	}
	slot := getenv("KD_CI_CREDENTIAL_SLOT")
	token := getenv(slot)
	if !slotRE.MatchString(slot) || token == "" || len(token) > 64<<10 || strings.ContainsAny(token, "\x00\r\n") || fields["protocol"] != "https" || fields["host"] != "github.com" || fields["path"] != getenv("KD_CI_CREDENTIAL_PATH") {
		return ErrRefused
	}
	_, e := io.WriteString(out, "username=x-access-token\npassword="+token+"\n\n")
	if e != nil {
		return ErrRefused
	}
	return nil
}
