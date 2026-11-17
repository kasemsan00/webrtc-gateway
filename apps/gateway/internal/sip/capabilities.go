package sip

const (
	// NOTIFY is accepted for the refer event of transfers the gateway sends.
	// REFER is only sent, not accepted (an inbound REFER gets 501), so it is
	// not advertised.
	sipAllowHeader = "INVITE, ACK, CANCEL, OPTIONS, BYE, MESSAGE, INFO, UPDATE, NOTIFY"
	// Keep Supported conservative: do not advertise 100rel or session timers
	// before those flows are implemented as first-class behavior.
	sipSupportedHeader = "outbound"
)

func sipAllowHeaderValue() string {
	return sipAllowHeader
}

func sipSupportedHeaderValue() string {
	return sipSupportedHeader
}
