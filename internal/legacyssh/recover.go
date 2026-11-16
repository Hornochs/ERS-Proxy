// Added by ERS-Proxy (not part of x/crypto): see README.md.

package legacyssh

import "fmt"

// recoverPacket runs one step of a connection's read or key exchange loop and
// turns a panic into an error, so that malformed data from an old switch ends
// that connection instead of the whole process (these loops run in their own
// goroutines, where the caller cannot recover).
func recoverPacket(f func() ([]byte, error)) (p []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			p, err = nil, fmt.Errorf("legacyssh: internal error: %v", r)
		}
	}()
	return f()
}
