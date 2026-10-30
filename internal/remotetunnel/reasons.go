package remotetunnel

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

// Sentinels the wizard's later steps return; ReasonOf turns each into a reason code.
var (
	ErrHostnameTaken     = errors.New("remote access: hostname already used by another record")
	ErrDomainElsewhere   = errors.New("remote access: domain belongs to another Cloudflare account")
	ErrNoAccounts        = errors.New("remote access: token sees no Cloudflare account")
	ErrAccountNotVisible = errors.New("remote access: chosen account is not visible to the token")
	// ErrDomainInvalid is the domain or label failing validation; it is an ErrConfiguration too.
	ErrDomainInvalid = fmt.Errorf("%w: domain or label is not a valid hostname", ErrConfiguration)
)

// ReasonOf maps a reconciliation error to the code stored on the row. It reads types and
// fields, never message text, because the code reaches the browser and the text must not.
func ReasonOf(err error) Reason {
	var api *cloudflareapi.APIError
	switch {
	case errors.Is(err, ErrHostnameTaken):
		return ReasonHostnameTaken
	case errors.Is(err, ErrDomainElsewhere):
		return ReasonDomainElsewhere
	case errors.Is(err, ErrNoAccounts):
		return ReasonNoAccounts
	case errors.Is(err, ErrAccountNotVisible):
		return ReasonAccountNotVisible
	case errors.Is(err, ErrDomainInvalid):
		return ReasonDomainInvalid
	case errors.Is(err, ErrOwnershipConflict):
		return ReasonNotOwned
	case errors.Is(err, ErrMemberLockout):
		return ReasonNoAdministrator
	case errors.Is(err, ErrRemotePresent):
		return ReasonOldAddressPresent
	case errors.As(err, &api) && (api.Status == 401 || api.Status == 403):
		return ReasonTokenRejected
	case transient(err):
		return ReasonCloudflareUnreachable
	}
	return ReasonUnknown
}

// transient is the one definition of an error worth retrying; the reason code and the retry
// schedule both read it, so a row cannot say unreachable while giving up.
func transient(err error) bool {
	var timeout net.Error
	return cloudflareapi.Retryable(err) || errors.Is(err, ErrRemotePresent) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout()
}
