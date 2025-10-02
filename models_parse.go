package rdapclient

import (
	"encoding/json"
	"errors"
)

// Object is a union interface implemented by all object classes.
type Object interface {
	GetObjectClassName() string
}

// ParseObject inspects objectClassName and returns a typed object per RFC 9083.
func ParseObject(m map[string]any) (Object, error) {
	if m == nil {
		return nil, errors.New("nil RDAP object")
	}
	ocn, _ := m["objectClassName"].(string)
	switch lower(ocn) {
	case "entity":
		var v Entity
		if err := decodeInto(m, &v); err != nil {
			return nil, err
		}
		if !v.Validate() {
			return nil, errors.New("invalid entity objectClassName")
		}
		return &v, nil
	case "domain":
		var v Domain
		if err := decodeInto(m, &v); err != nil {
			return nil, err
		}
		if !v.Validate() {
			return nil, errors.New("invalid domain objectClassName")
		}
		return &v, nil
	case "nameserver":
		var v Nameserver
		if err := decodeInto(m, &v); err != nil {
			return nil, err
		}
		if !v.Validate() {
			return nil, errors.New("invalid nameserver objectClassName")
		}
		return &v, nil
	case "ip network":
		var v IPNetwork
		if err := decodeInto(m, &v); err != nil {
			return nil, err
		}
		if !v.Validate() {
			return nil, errors.New("invalid ip network objectClassName")
		}
		return &v, nil
	case "autnum":
		var v Autnum
		if err := decodeInto(m, &v); err != nil {
			return nil, err
		}
		if !v.Validate() {
			return nil, errors.New("invalid autnum objectClassName")
		}
		return &v, nil
	default:
		return nil, errors.New("unknown RDAP objectClassName: " + ocn)
	}
}

func decodeInto(m map[string]any, v any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
