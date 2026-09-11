//go:build darwin

package securefs

import "golang.org/x/sys/unix"

func statMtime(stat unix.Stat_t) (sec, nsec int64) {
	return int64(stat.Mtimespec.Sec), int64(stat.Mtimespec.Nsec)
}
