package provider

import "regexp"

var secretHeader = regexp.MustCompile(`(?i)auth|key|token|secret|cookie|session|password`)

// SecretHeader reports whether a header or an environment variable by that
// name looks like it holds a key, for redaction and local configuration editing.
func SecretHeader(name string) bool { return secretHeader.MatchString(name) }
