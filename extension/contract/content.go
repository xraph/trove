package contract

import "net/url"

// Content is what the content intents and the content route share: the
// ticket signer, where the route is mounted, and the upload cap.
type Content struct {
	// Signer mints and checks tickets. Required.
	Signer *Signer

	// Path is where the content route is mounted, for example
	// "/dashboard/trove/content".
	Path string

	// MaxUploadBytes is the largest upload a ticket will allow. Every
	// Trove middleware buffers a whole object in memory, which is why this
	// is small by default.
	MaxUploadBytes int64

	// PerProcessSecret is true when Signer's key was generated at start
	// rather than configured, so a ticket verifies only in this process.
	PerProcessSecret bool
}

// TicketHeader is the request header that carries an upload ticket. Upload
// tickets never ride in the URL: forge's tracing records every request's
// query string, and an upload ticket lives 15 minutes and may allow an
// overwrite.
const TicketHeader = "X-Trove-Ticket"

// URL is the content route with token attached. Only download and preview
// tickets travel this way: they are read-only and live 60 seconds, and a
// plain <a download> link cannot send a header.
func (c *Content) URL(token string) string {
	return c.Path + "?t=" + url.QueryEscape(token)
}
