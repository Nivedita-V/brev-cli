//go:build !windows

package nvsync

import "syscall"

func execProxyProcess(path string, argv []string, env []string) error {
	return syscall.Exec(path, argv, env) //nolint:gosec // path is the fixed system OpenSSH executable
}
