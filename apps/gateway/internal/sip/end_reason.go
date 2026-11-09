package sip

// Reasons a client may get in the endReason of an ended state message.
const (
	endReasonBusy               = "busy"
	endReasonDeclined           = "declined"
	endReasonNotFound           = "not_found"
	endReasonInvalidNumber      = "invalid_number"
	endReasonForbidden          = "forbidden"
	endReasonUnavailable        = "unavailable"
	endReasonNoAnswer           = "no_answer"
	endReasonIncompatible       = "incompatible"
	endReasonAuthFailed         = "auth_failed"
	endReasonServiceUnavailable = "service_unavailable"
	endReasonFailed             = "failed"
)

// endReasonForSIPStatus maps a final failure response of an outbound INVITE.
func endReasonForSIPStatus(status int) string {
	switch status {
	case 486, 600:
		return endReasonBusy
	case 603:
		return endReasonDeclined
	case 404, 604:
		return endReasonNotFound
	case 484:
		return endReasonInvalidNumber
	case 401, 403, 407:
		return endReasonForbidden
	case 480:
		return endReasonUnavailable
	case 408:
		return endReasonNoAnswer
	case 488, 606:
		return endReasonIncompatible
	}
	if status >= 500 && status < 600 {
		return endReasonServiceUnavailable
	}
	return endReasonFailed
}
