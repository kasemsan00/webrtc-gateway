package sip

import (
	"github.com/emiago/sipgo/sip"
)

// DialogState holds extracted SIP dialog information
type DialogState struct {
	FromTag       string
	ToTag         string
	RemoteContact string
	RouteSet      []string
	CallID        string
}

// ExtractDialogStateFromResponse extracts dialog state from SIP response
func ExtractDialogStateFromResponse(res *sip.Response) (DialogState, error) {
	state := DialogState{}

	// Extract Call-ID
	if callID := res.CallID(); callID != nil {
		state.CallID = callID.Value()
	}

	// Extract To tag
	if toHeader := res.To(); toHeader != nil && toHeader.Params != nil {
		if tag, ok := toHeader.Params.Get("tag"); ok {
			state.ToTag = tag
		}
	}

	// Extract remote contact
	if contactHeaders := res.GetHeaders("Contact"); len(contactHeaders) > 0 {
		state.RemoteContact = contactHeaders[0].Value()
	}

	// Extract and reverse Record-Route headers
	state.RouteSet = ExtractRouteSet(res.GetHeaders("Record-Route"))

	return state, nil
}

// ExtractDialogStateFromINVITE extracts dialog state from INVITE request
func ExtractDialogStateFromINVITE(req *sip.Request) (DialogState, error) {
	state := DialogState{}

	// Extract Call-ID
	if callID := req.CallID(); callID != nil {
		state.CallID = callID.Value()
	}

	// Extract From tag
	if fromHeader := req.From(); fromHeader != nil && fromHeader.Params != nil {
		if tag, ok := fromHeader.Params.Get("tag"); ok {
			state.FromTag = tag
		}
	}

	// Extract remote contact
	if contactHeaders := req.GetHeaders("Contact"); len(contactHeaders) > 0 {
		state.RemoteContact = contactHeaders[0].Value()
	}

	// Extract and reverse Record-Route headers
	state.RouteSet = ExtractRouteSet(req.GetHeaders("Record-Route"))

	return state, nil
}

// ExtractRouteSet reverses Record-Route headers to create Route set (RFC 3261)
func ExtractRouteSet(recordRoutes []sip.Header) []string {
	routeSet := make([]string, 0, len(recordRoutes))
	for i := len(recordRoutes) - 1; i >= 0; i-- {
		routeSet = append(routeSet, recordRoutes[i].Value())
	}
	return routeSet
}
