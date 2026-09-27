package config

import "os"

// Windows ignores most Unix mode bits. The user's directory ACL is responsible
// for protecting credentials; portable stdlib APIs cannot validate that ACL.
func privateFile(info os.FileInfo) error { return nil }
