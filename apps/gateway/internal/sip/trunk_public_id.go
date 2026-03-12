package sip

import "github.com/google/uuid"

// NormalizeTrunkPublicID validates and returns a canonical lowercase UUID string.
func NormalizeTrunkPublicID(publicID string) (string, bool) {
	parsed, err := uuid.Parse(publicID)
	if err != nil {
		return "", false
	}
	return parsed.String(), true
}

// IsValidTrunkPublicID checks whether a trunk public ID is a valid UUID.
func IsValidTrunkPublicID(publicID string) bool {
	_, ok := NormalizeTrunkPublicID(publicID)
	return ok
}
