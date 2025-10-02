package rdapclient

import "context"

// rdapBaseForIP resolves the RDAP base for a given IP or CIDR using IANA ipv4/ipv6 bootstrap.
func (c *Client) rdapBaseForIP(ctx context.Context, ipOrCIDR string) (string, error) {
	return c.resolveBaseFromBootstrapIP(ctx, ipOrCIDR)
}

func (c *Client) IP(ctx context.Context, ipOrCIDR string) (*IPNetwork, error) {
	base, err := c.rdapBaseForIP(ctx, ipOrCIDR)
	if err != nil {
		return nil, err
	}
	u := mustJoin(base, "/ip/", ipOrCIDR)
	m, _, err := c.getJSON(ctx, u)
	if err != nil {
		return nil, err
	}
	obj, err := ParseObject(m)
	if err != nil {
		return nil, err
	}
	ipn, ok := obj.(*IPNetwork)
	if !ok {
		return nil, ErrUnexpectedObject("ip network")
	}
	return ipn, nil
}
