//go:build windows

package nvsync

import "fmt"

func execProxyProcess(string, []string, []string) error {
	return fmt.Errorf("Brev SSH route proxy is unsupported on Windows")
}
