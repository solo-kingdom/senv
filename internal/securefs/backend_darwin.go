//go:build darwin

package securefs

func platformOpenRead(rootFD int, segments []string) (int, error) {
	return openReadFallback(rootFD, segments)
}

func platformOpenParent(rootFD int, segments []string) (int, error) {
	return openParentFallback(rootFD, segments)
}
