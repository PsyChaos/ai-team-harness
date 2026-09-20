package worker

import (
	"os"
	"syscall"
)

func linked(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && !info.IsDir() && stat.Nlink > 1
}
