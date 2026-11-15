package appdir

// Blocked is "" on Windows: a profile folder there isn't taken over by a
// command run as Administrator the way sudo leaves ~/.config root's.
func Blocked() string { return "" }

// Own says whether p is uni-switch's own folder or inside it.
func Own(p string) bool { return false }

// Mine says whether this user owns p.
func Mine(p string) bool { return false }
