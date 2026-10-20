package config

// Switches reports the two processing switches stored in a mode.
// none is both off. compress and webp are each one switch. both is both on.
func Switches(mode string) (compress, webp bool) {
	switch mode {
	case "compress":
		return true, false
	case "webp":
		return false, true
	case "both":
		return true, true
	default:
		return false, false
	}
}

// ModeFromSwitches stores the two switches as one mode.
func ModeFromSwitches(compress, webp bool) string {
	switch {
	case compress && webp:
		return "both"
	case compress:
		return "compress"
	case webp:
		return "webp"
	default:
		return "none"
	}
}

// EffectiveMode uses an explicit flag when one was given, otherwise the saved mode.
func EffectiveMode(saved, flag string) string {
	if flag != "" {
		return flag
	}
	if saved == "" {
		return "none"
	}
	return saved
}

// EffectiveQuality uses an explicit flag when one was given, otherwise the saved quality.
func EffectiveQuality(saved, flag int) int {
	if flag != 0 {
		return flag
	}
	if saved == 0 {
		return 80
	}
	return saved
}
