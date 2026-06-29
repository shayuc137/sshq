//go:build windows

package cli

import "os"

func openControlTerminal() (*os.File, error) {
	return os.OpenFile(`CONIN$`, os.O_RDWR, 0)
}
