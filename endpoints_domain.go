package rdapclient

import "context"

// Domain returns a typed RDAP Domain per RFC 9083.
func (c *Client) Domain(ctx context.Context, fqdn string) (*Domain, error) {
	base, err := c.rdapBaseForDomain(ctx, fqdn)
	if err != nil {
		return nil, err
	}
	u := mustJoin(base, "/domain/", fqdn)
	raw, _, err := c.getJSON(ctx, u)
	if err != nil {
		return nil, err
	}
	obj, err := ParseObject(raw)
	if err != nil {
		return nil, err
	}
	d, ok := obj.(*Domain)
	if !ok {
		return nil, ErrUnexpectedObject("domain")
	}
	return d, nil
}
