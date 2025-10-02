package rdapclient

import "fmt"

// ErrUnexpectedObject indicates the RDAP response was not the expected object class.
type ErrUnexpectedObject string

func (e ErrUnexpectedObject) Error() string {
	return fmt.Sprintf("unexpected RDAP objectClassName, want %s", string(e))
}
