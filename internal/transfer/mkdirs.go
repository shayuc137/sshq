package transfer

import "fmt"

type RemoteParentMissingError struct {
	Path string
}

func (e *RemoteParentMissingError) Error() string {
	return fmt.Sprintf("remote destination parent directory does not exist: %s", e.Path)
}
