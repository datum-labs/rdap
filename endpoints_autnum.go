package rdapclient

import (
	"context"
	"strconv"
	"strings"
)

// rdapBaseForASN resolves the RDAP base for an ASN via IANA asn.json.
func (c *Client) rdapBaseForASN(ctx context.Context, asn string) (string, error) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(asn), "AS"))
	n, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil {
		return "", err
	}
	return c.resolveBaseFromBootstrapASN(ctx, n)
}

func (c *Client) Autnum(ctx context.Context, asn string) (*Autnum, error) {
	trimmed := strings.TrimPrefix(strings.ToUpper(asn), "AS")
	if _, err := strconv.ParseUint(trimmed, 10, 64); err != nil {
		return nil, err
	}
	base, err := c.rdapBaseForASN(ctx, trimmed)
	if err != nil {
		return nil, err
	}
	u := mustJoin(base, "/autnum/", trimmed)
	m, _, err := c.getJSON(ctx, u)
	if err != nil {
		return nil, err
	}
	obj, err := ParseObject(m)
	if err != nil {
		return nil, err
	}
	a, ok := obj.(*Autnum)
	if !ok {
		return nil, ErrUnexpectedObject("autnum")
	}
	return a, nil
}
