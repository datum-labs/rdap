package rdapclient

import "context"

// Entity queries an entity handle and returns a typed Entity; tldHint helps pick the right registry base.
func (c *Client) Entity(ctx context.Context, handle, tldHint string) (*Entity, error) {
	var base string
	var err error
	if tl := trimDotLower(tldHint); tl != "" {
		base, err = c.rdapBaseForTLD(ctx, tl)
	}
	if base == "" || err != nil {
		base = "https://rdap.org"
	}
	u := mustJoin(base, "/entity/", handle)
	m, _, err := c.getJSON(ctx, u)
	if err != nil {
		return nil, err
	}
	obj, err := ParseObject(m)
	if err != nil {
		return nil, err
	}
	e, ok := obj.(*Entity)
	if !ok {
		return nil, ErrUnexpectedObject("entity")
	}
	return e, nil
}
