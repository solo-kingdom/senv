//go:build darwin

package securefs

import "golang.org/x/sys/unix"

func statMtime(stat unix.Stat_t) (sec, nsec int64) {
	return int64(stat.Mtim.Sec), int64(stat.Mtim.Nsec)
}
