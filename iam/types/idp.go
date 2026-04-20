package types

// IDPUser is the normalized profile returned by any external IDP.
// Different providers return different formats and the adapter for each IDP
// is responsible for normalizing the data into this type.
type IDPUser struct {
	ExternalID    string
	Provider      string
	Email         string
	EmailVerified bool
	Name          string
	PictureURL    string
	RawClaims     map[string]any // original claims from the IDP for reference
}
