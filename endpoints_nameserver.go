package rdapclient

import "context"

func (c *Client) Nameserver(ctx context.Context, host string) (*Nameserver, error) {
	base, err := c.rdapBaseForDomain(ctx, host)
	if err != nil || base == "" {
		base = "https://rdap.org"
	}
	u := mustJoin(base, "/nameserver/", host)
	m, _, err := c.getJSON(ctx, u)
	if err != nil {
		return nil, err
	}
	obj, err := ParseObject(m)
	if err != nil {
		return nil, err
	}
	ns, ok := obj.(*Nameserver)
	if !ok {
		return nil, ErrUnexpectedObject("nameserver")
	}
	return ns, nil
}
