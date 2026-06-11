package sip

const (
	sipAllowHeader = "INVITE, ACK, CANCEL, OPTIONS, BYE, MESSAGE, INFO, UPDATE"
	// Keep Supported conservative: do not advertise 100rel, REFER/transfer, or
	// session timers before those flows are implemented as first-class behavior.
	sipSupportedHeader = "outbound"
)

func sipAllowHeaderValue() string {
	return sipAllowHeader
}

func sipSupportedHeaderValue() string {
	return sipSupportedHeader
}
