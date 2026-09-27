package manager

import "regexp"

// browserSessionPattern bounds an agent-browser session name. The name becomes a path twice in
// the box: the live view's stream file and the session's Chrome profile directory.
var browserSessionPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,48}$`)

// ValidBrowserSession reports whether name may name a browser recipe session.
func ValidBrowserSession(name string) bool { return browserSessionPattern.MatchString(name) }
