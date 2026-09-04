//go:build linux || darwin

package securefs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/sys/unix"
)

func platformOpenRoot(path string) (int, error) {
	if path == "" {
		return -1, &PathError{Op: "open root", Path: path, Err: ErrContainment}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return -1, &PathError{Op: "open root", Path: path, Err: fmt.Errorf("%w: %v", ErrContainment, err)}
	}
	clean := filepath.Clean(absolute)
	info, err := os.Lstat(clean)
	if err != nil {
		return -1, syscallPathError("inspect root", clean, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return -1, &PathError{Op: "open root", Path: clean, Err: ErrSymlink}
	}
	fd, err := unix.Open(clean, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, syscallPathError("open root", clean, err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = unix.Close(fd)
		return -1, syscallPathError("stat root", clean, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(fd)
		return -1, &PathError{Op: "open root", Path: clean, Err: ErrRootChanged}
	}
	return fd, nil
}

func platformClose(fd int) error {
	if err := unix.Close(fd); err != nil {
		return &PathError{Op: "close root", Err: err}
	}
	return nil
}

func platformRead(rootFD int, segments []string) ([]byte, error) {
	data, _, err := platformReadWithMode(rootFD, segments)
	return data, err
}

func platformReadWithMode(rootFD int, segments []string) ([]byte, fs.FileMode, error) {
	fd, err := platformOpenRead(rootFD, segments)
	if err != nil {
		return nil, 0, err
	}
	file := os.NewFile(uintptr(fd), displayPath(segments))
	if file == nil {
		_ = unix.Close(fd)
		return nil, 0, &PathError{Op: "read", Path: displayPath(segments), Err: errors.New("invalid file descriptor")}
	}
	defer file.Close()

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, 0, syscallPathError("stat", displayPath(segments), err)
	}
	if uint32(stat.Mode)&uint32(unix.S_IFMT) != uint32(unix.S_IFREG) {
		return nil, 0, &PathError{Op: "read", Path: displayPath(segments), Err: ErrNotRegular}
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, 0, &PathError{Op: "read", Path: displayPath(segments), Err: err}
	}
	return data, fs.FileMode(stat.Mode & 0o777), nil
}

func platformMode(rootFD int, segments []string) (fs.FileMode, error) {
	fd, err := platformOpenRead(rootFD, segments)
	if err != nil {
		return 0, err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return 0, syscallPathError("inspect mode", displayPath(segments), err)
	}
	if uint32(stat.Mode)&uint32(unix.S_IFMT) != uint32(unix.S_IFREG) {
		return 0, &PathError{Op: "inspect mode", Path: displayPath(segments), Err: ErrNotRegular}
	}
	return fs.FileMode(stat.Mode & 0o777), nil
}

func platformReadDir(rootFD int, segments []string) ([]DirEntry, error) {
	directoryFD, err := platformOpenParent(rootFD, segments)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(directoryFD), displayPath(segments))
	if file == nil {
		_ = unix.Close(directoryFD)
		return nil, &PathError{Op: "read directory", Path: displayPath(segments), Err: errors.New("invalid file descriptor")}
	}
	defer file.Close()
	entries, readErr := file.ReadDir(-1)
	if readErr != nil {
		return nil, &PathError{Op: "read directory", Path: displayPath(segments), Err: readErr}
	}

	result := make([]DirEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if err := ValidateSegment(name); err != nil {
			return nil, &PathError{Op: "read directory", Path: displayPath(appendPath(segments, name)), Err: err}
		}
		var stat unix.Stat_t
		if err := unix.Fstatat(directoryFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return nil, syscallPathError("inspect directory entry", displayPath(appendPath(segments, name)), err)
		}
		switch uint32(stat.Mode) & uint32(unix.S_IFMT) {
		case uint32(unix.S_IFLNK):
			return nil, &PathError{Op: "read directory", Path: displayPath(appendPath(segments, name)), Err: ErrSymlink}
		case uint32(unix.S_IFDIR):
			result = append(result, DirEntry{Name: name, IsDir: true})
		case uint32(unix.S_IFREG):
			result = append(result, DirEntry{Name: name})
		default:
			return nil, &PathError{Op: "read directory", Path: displayPath(appendPath(segments, name)), Err: ErrNotRegular}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func platformEnsureDir(rootFD int, segments []string, mode fs.FileMode, tightenFinal bool) error {
	current, err := unix.Dup(rootFD)
	if err != nil {
		return syscallPathError("duplicate root", "", err)
	}
	unix.CloseOnExec(current)
	defer func() { _ = unix.Close(current) }()

	for index, segment := range segments {
		created := false
		var stat unix.Stat_t
		err := unix.Fstatat(current, segment, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			mkdirErr := unix.Mkdirat(current, segment, uint32(mode.Perm()))
			if mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				return syscallPathError("create directory", displayPath(segments[:index+1]), mkdirErr)
			}
			created = mkdirErr == nil
			if err := unix.Fstatat(current, segment, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return syscallPathError("inspect directory", displayPath(segments[:index+1]), err)
			}
		} else if err != nil {
			return syscallPathError("inspect directory", displayPath(segments[:index+1]), err)
		}
		switch uint32(stat.Mode) & uint32(unix.S_IFMT) {
		case uint32(unix.S_IFLNK):
			return &PathError{Op: "ensure directory", Path: displayPath(segments[:index+1]), Err: ErrSymlink}
		case uint32(unix.S_IFDIR):
		default:
			return &PathError{Op: "ensure directory", Path: displayPath(segments[:index+1]), Err: ErrContainment}
		}
		next, openErr := unix.Openat(current, segment, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return syscallPathError("open directory", displayPath(segments[:index+1]), openErr)
		}
		if created {
			if err := unix.Fsync(current); err != nil {
				_ = unix.Close(next)
				return syscallPathError("sync directory", displayPath(segments[:index]), err)
			}
		}
		if index == len(segments)-1 && (created || tightenFinal) {
			finalMode := uint32(mode.Perm()) & uint32(stat.Mode) & 0o777
			if created {
				finalMode = uint32(mode.Perm())
			}
			if err := unix.Fchmod(next, finalMode); err != nil {
				_ = unix.Close(next)
				return syscallPathError("set directory mode", displayPath(segments), err)
			}
			if err := unix.Fsync(next); err != nil {
				_ = unix.Close(next)
				return syscallPathError("sync directory", displayPath(segments), err)
			}
		}
		_ = unix.Close(current)
		current = next
	}
	return nil
}

func openParentFallback(rootFD int, parentSegments []string) (int, error) {
	current, err := unix.Dup(rootFD)
	if err != nil {
		return -1, syscallPathError("duplicate root", "", err)
	}
	unix.CloseOnExec(current)
	for index, segment := range parentSegments {
		var entryStat unix.Stat_t
		if statErr := unix.Fstatat(current, segment, &entryStat, unix.AT_SYMLINK_NOFOLLOW); statErr != nil {
			_ = unix.Close(current)
			return -1, syscallPathError("inspect directory", displayPath(parentSegments[:index+1]), statErr)
		}
		if entryStat.Mode&unix.S_IFMT == unix.S_IFLNK {
			_ = unix.Close(current)
			return -1, &PathError{Op: "open directory", Path: displayPath(parentSegments[:index+1]), Err: ErrSymlink}
		}
		next, openErr := unix.Openat(current, segment, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(current)
		if openErr != nil {
			return -1, syscallPathError("open directory", displayPath(parentSegments[:index+1]), openErr)
		}
		var stat unix.Stat_t
		if statErr := unix.Fstat(next, &stat); statErr != nil {
			_ = unix.Close(next)
			return -1, syscallPathError("stat directory", displayPath(parentSegments[:index+1]), statErr)
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
			_ = unix.Close(next)
			return -1, &PathError{Op: "open directory", Path: displayPath(parentSegments[:index+1]), Err: ErrContainment}
		}
		current = next
	}
	return current, nil
}

func openReadFallback(rootFD int, segments []string) (int, error) {
	parent, err := openParentFallback(rootFD, segments[:len(segments)-1])
	if err != nil {
		return -1, err
	}
	defer unix.Close(parent)
	fd, err := unix.Openat(parent, segments[len(segments)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, syscallPathError("open", displayPath(segments), err)
	}
	return fd, nil
}

func syscallPathError(op, path string, err error) error {
	category := err
	switch {
	case errors.Is(err, unix.ELOOP):
		category = fmt.Errorf("%w: %v", ErrSymlink, err)
	case errors.Is(err, unix.EXDEV):
		category = fmt.Errorf("%w: %v", ErrContainment, err)
	case errors.Is(err, unix.ENOTDIR):
		category = fmt.Errorf("%w: %v", ErrContainment, err)
	}
	return &PathError{Op: op, Path: path, Err: category}
}

func platformAtomicWrite(rootFD int, segments []string, data []byte, mode fs.FileMode, hooks *atomicWriteHooks) error {
	parentSegments := segments[:len(segments)-1]
	target := segments[len(segments)-1]
	parent, err := platformOpenParent(rootFD, parentSegments)
	if err != nil {
		return err
	}
	defer unix.Close(parent)

	finalMode, err := restrictedTargetMode(parent, target, uint32(mode.Perm()))
	if err != nil {
		return err
	}

	tempName, tempFD, err := createExclusiveTemp(parent, hooks)
	if err != nil {
		return &PathError{Op: "create temporary file", Path: displayPath(segments), Err: err}
	}
	renamed := false
	defer func() {
		_ = unix.Close(tempFD)
		if !renamed {
			_ = unix.Unlinkat(parent, tempName, 0)
		}
	}()

	if err := unix.Fchmod(tempFD, 0o600); err != nil {
		return syscallPathError("secure temporary file", displayPath(segments), err)
	}
	if err := writeComplete(tempFD, data, hooks); err != nil {
		return &PathError{Op: "write temporary file", Path: displayPath(segments), Err: err}
	}
	// Recheck immediately before committing. renameat replaces a link itself
	// rather than following it, while this check also rejects a pre-existing
	// link as a policy violation and observes a newly stricter target mode.
	finalMode, err = restrictedTargetMode(parent, target, finalMode)
	if err != nil {
		return err
	}
	if err := unix.Fchmod(tempFD, finalMode); err != nil {
		return syscallPathError("set mode", displayPath(segments), err)
	}
	if err := callFileSync(tempFD, hooks); err != nil {
		return syscallPathError("sync temporary file", displayPath(segments), err)
	}
	if err := unix.Close(tempFD); err != nil {
		tempFD = -1
		return syscallPathError("close temporary file", displayPath(segments), err)
	}
	tempFD = -1

	if err := callRename(parent, tempName, parent, target, hooks); err != nil {
		return syscallPathError("rename", displayPath(segments), err)
	}
	renamed = true
	if err := callDirSync(parent, hooks); err != nil {
		return syscallPathError("sync directory", displayPath(parentSegments), err)
	}
	return nil
}

func restrictedTargetMode(parent int, target string, requested uint32) (uint32, error) {
	var stat unix.Stat_t
	err := unix.Fstatat(parent, target, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return requested & 0o777, nil
	}
	if err != nil {
		return 0, syscallPathError("inspect target", target, err)
	}
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFLNK:
		return 0, &PathError{Op: "inspect target", Path: target, Err: ErrSymlink}
	case unix.S_IFREG:
		return requested & uint32(stat.Mode) & 0o777, nil
	default:
		return 0, &PathError{Op: "inspect target", Path: target, Err: ErrNotRegular}
	}
}

func createExclusiveTemp(parent int, hooks *atomicWriteHooks) (string, int, error) {
	for attempt := 0; attempt < 128; attempt++ {
		randomHex, err := randomTempHex(hooks)
		if err != nil {
			return "", -1, err
		}
		name := ".senv-tmp-" + randomHex
		fd, err := unix.Openat(parent, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
		if err == nil {
			return name, fd, nil
		}
		if !errors.Is(err, unix.EEXIST) {
			return "", -1, err
		}
	}
	return "", -1, errors.New("temporary file name collision limit reached")
}

func randomTempHex(hooks *atomicWriteHooks) (string, error) {
	if hooks != nil && hooks.randomHex != nil {
		return hooks.randomHex()
	}
	var random [16]byte
	if _, err := io.ReadFull(rand.Reader, random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

func writeComplete(fd int, data []byte, hooks *atomicWriteHooks) error {
	write := unix.Write
	if hooks != nil && hooks.write != nil {
		write = hooks.write
	}
	for len(data) > 0 {
		n, err := write(fd, data)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if n <= 0 || n > len(data) {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func callFileSync(fd int, hooks *atomicWriteHooks) error {
	if hooks != nil && hooks.fileSync != nil {
		return hooks.fileSync(fd)
	}
	return unix.Fsync(fd)
}

func callDirSync(fd int, hooks *atomicWriteHooks) error {
	if hooks != nil && hooks.dirSync != nil {
		return hooks.dirSync(fd)
	}
	return unix.Fsync(fd)
}

func callRename(oldDir int, oldName string, newDir int, newName string, hooks *atomicWriteHooks) error {
	if hooks != nil && hooks.rename != nil {
		return hooks.rename(oldDir, oldName, newDir, newName)
	}
	return unix.Renameat(oldDir, oldName, newDir, newName)
}

func platformRemove(rootFD int, segments []string) error {
	parentSegments := segments[:len(segments)-1]
	name := segments[len(segments)-1]
	parent, err := platformOpenParent(rootFD, parentSegments)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	if _, err := inspectEntry(parent, name, false); err != nil {
		return err
	}
	if err := unix.Unlinkat(parent, name, 0); err != nil {
		return syscallPathError("remove", displayPath(segments), err)
	}
	if err := unix.Fsync(parent); err != nil {
		return syscallPathError("sync directory", displayPath(parentSegments), err)
	}
	return nil
}

func platformRename(rootFD int, oldSegments, newSegments []string) error {
	oldParentSegments := oldSegments[:len(oldSegments)-1]
	newParentSegments := newSegments[:len(newSegments)-1]
	oldName := oldSegments[len(oldSegments)-1]
	newName := newSegments[len(newSegments)-1]

	oldParent, err := platformOpenParent(rootFD, oldParentSegments)
	if err != nil {
		return err
	}
	defer unix.Close(oldParent)
	newParent, err := platformOpenParent(rootFD, newParentSegments)
	if err != nil {
		return err
	}
	defer unix.Close(newParent)

	if _, err := inspectEntry(oldParent, oldName, true); err != nil {
		return err
	}
	if _, err := inspectOptionalRenameTarget(newParent, newName); err != nil {
		return err
	}
	if err := unix.Renameat(oldParent, oldName, newParent, newName); err != nil {
		return syscallPathError("rename", displayPath(oldSegments)+" -> "+displayPath(newSegments), err)
	}
	if err := unix.Fsync(oldParent); err != nil {
		return syscallPathError("sync directory", displayPath(oldParentSegments), err)
	}
	if err := unix.Fsync(newParent); err != nil {
		return syscallPathError("sync directory", displayPath(newParentSegments), err)
	}
	return nil
}

func inspectEntry(parent int, name string, allowDirectory bool) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstatat(parent, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return stat, syscallPathError("inspect entry", name, err)
	}
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFLNK:
		return stat, &PathError{Op: "inspect entry", Path: name, Err: ErrSymlink}
	case unix.S_IFREG:
		return stat, nil
	case unix.S_IFDIR:
		if allowDirectory {
			return stat, nil
		}
	}
	return stat, &PathError{Op: "inspect entry", Path: name, Err: ErrNotRegular}
}

func inspectOptionalRenameTarget(parent int, name string) (bool, error) {
	var stat unix.Stat_t
	err := unix.Fstatat(parent, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, syscallPathError("inspect rename target", name, err)
	}
	if stat.Mode&unix.S_IFMT == unix.S_IFLNK {
		return true, &PathError{Op: "inspect rename target", Path: name, Err: ErrSymlink}
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG && stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return true, &PathError{Op: "inspect rename target", Path: name, Err: ErrNotRegular}
	}
	return true, nil
}

type removeTreeNode struct {
	fd       int
	stat     unix.Stat_t
	files    []removeTreeFile
	children []removeTreeChild
}

type removeTreeFile struct {
	name string
	stat unix.Stat_t
}

type removeTreeChild struct {
	name string
	node *removeTreeNode
}

// removeTreeBeforeQuarantine is a package-private test seam. Production never
// sets it; tests use it to model a path replacement after a successful tree
// preflight but before the atomic quarantine rename.
var removeTreeBeforeQuarantine func()

func platformRemoveTree(rootFD int, segments []string) error {
	parentSegments := segments[:len(segments)-1]
	name := segments[len(segments)-1]
	parent, err := platformOpenParent(rootFD, parentSegments)
	if err != nil {
		return err
	}
	defer unix.Close(parent)

	node, err := preflightTree(parent, name, segments)
	if err != nil {
		return err
	}
	defer node.close()
	if err := verifyEntryIdentity(parent, name, node.stat, unix.S_IFDIR); err != nil {
		return err
	}
	if removeTreeBeforeQuarantine != nil {
		removeTreeBeforeQuarantine()
	}

	quarantine, err := createRemoveTreeQuarantine(parent)
	if err != nil {
		return err
	}
	if err := unix.Renameat(parent, name, parent, quarantine); err != nil {
		return syscallPathError("quarantine tree", displayPath(segments), err)
	}
	if err := verifyEntryIdentity(parent, quarantine, node.stat, unix.S_IFDIR); err != nil {
		return err
	}

	quarantinePath := appendPath(parentSegments, quarantine)
	if err := removeTreeContents(node, quarantinePath); err != nil {
		return err
	}
	if err := verifyEntryIdentity(parent, quarantine, node.stat, unix.S_IFDIR); err != nil {
		return err
	}
	if err := unix.Unlinkat(parent, quarantine, unix.AT_REMOVEDIR); err != nil {
		return syscallPathError("remove quarantined tree", displayPath(quarantinePath), err)
	}
	if err := unix.Fsync(parent); err != nil {
		return syscallPathError("sync directory", displayPath(parentSegments), err)
	}
	return nil
}

// createRemoveTreeQuarantine reserves an unpredictable, private sibling name.
// The tree is moved only after this reservation and its full preflight succeed.
func createRemoveTreeQuarantine(parent int) (string, error) {
	for attempt := 0; attempt < 128; attempt++ {
		random, err := randomTempHex(nil)
		if err != nil {
			return "", err
		}
		name := ".senv-quarantine-" + random
		exists, err := inspectOptionalRenameTarget(parent, name)
		if err != nil {
			return "", err
		}
		if !exists {
			return name, nil
		}
	}
	return "", errors.New("quarantine name collision limit reached")
}

func preflightTree(parent int, name string, path []string) (*removeTreeNode, error) {
	stat, err := inspectEntry(parent, name, true)
	if err != nil {
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return nil, &PathError{Op: "remove tree", Path: displayPath(path), Err: ErrNotRegular}
	}
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, syscallPathError("open directory", displayPath(path), err)
	}
	node := &removeTreeNode{fd: fd, stat: stat}
	if err := verifyFDIdentity(fd, stat, unix.S_IFDIR, displayPath(path)); err != nil {
		node.close()
		return nil, err
	}

	duplicate, err := unix.Dup(fd)
	if err != nil {
		node.close()
		return nil, syscallPathError("duplicate directory", displayPath(path), err)
	}
	unix.CloseOnExec(duplicate)
	directory := os.NewFile(uintptr(duplicate), displayPath(path))
	if directory == nil {
		_ = unix.Close(duplicate)
		node.close()
		return nil, &PathError{Op: "read directory", Path: displayPath(path), Err: errors.New("invalid file descriptor")}
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if readErr != nil {
		node.close()
		return nil, &PathError{Op: "read directory", Path: displayPath(path), Err: readErr}
	}
	if closeErr != nil {
		node.close()
		return nil, &PathError{Op: "close directory", Path: displayPath(path), Err: closeErr}
	}

	for _, entry := range entries {
		entryName := entry.Name()
		if err := ValidateSegment(entryName); err != nil {
			node.close()
			return nil, &PathError{Op: "preflight tree", Path: displayPath(appendPath(path, entryName)), Err: err}
		}
		var entryStat unix.Stat_t
		if err := unix.Fstatat(fd, entryName, &entryStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			node.close()
			return nil, syscallPathError("inspect tree entry", displayPath(appendPath(path, entryName)), err)
		}
		switch entryStat.Mode & unix.S_IFMT {
		case unix.S_IFLNK:
			node.close()
			return nil, &PathError{Op: "preflight tree", Path: displayPath(appendPath(path, entryName)), Err: ErrSymlink}
		case unix.S_IFREG:
			node.files = append(node.files, removeTreeFile{name: entryName, stat: entryStat})
		case unix.S_IFDIR:
			child, err := preflightTree(fd, entryName, appendPath(path, entryName))
			if err != nil {
				node.close()
				return nil, err
			}
			node.children = append(node.children, removeTreeChild{name: entryName, node: child})
		default:
			node.close()
			return nil, &PathError{Op: "preflight tree", Path: displayPath(appendPath(path, entryName)), Err: ErrNotRegular}
		}
	}
	return node, nil
}

func removeTreeContents(node *removeTreeNode, path []string) error {
	for _, file := range node.files {
		entryPath := appendPath(path, file.name)
		if err := verifyEntryIdentity(node.fd, file.name, file.stat, unix.S_IFREG); err != nil {
			return err
		}
		if err := unix.Unlinkat(node.fd, file.name, 0); err != nil {
			return syscallPathError("remove", displayPath(entryPath), err)
		}
	}
	for _, child := range node.children {
		childPath := appendPath(path, child.name)
		if err := verifyEntryIdentity(node.fd, child.name, child.node.stat, unix.S_IFDIR); err != nil {
			return err
		}
		if err := removeTreeContents(child.node, childPath); err != nil {
			return err
		}
		if err := verifyEntryIdentity(node.fd, child.name, child.node.stat, unix.S_IFDIR); err != nil {
			return err
		}
		if err := unix.Unlinkat(node.fd, child.name, unix.AT_REMOVEDIR); err != nil {
			return syscallPathError("remove directory", displayPath(childPath), err)
		}
	}
	if err := unix.Fsync(node.fd); err != nil {
		return syscallPathError("sync directory", displayPath(path), err)
	}
	return nil
}

func verifyEntryIdentity(parent int, name string, expected unix.Stat_t, expectedType uint32) error {
	var current unix.Stat_t
	if err := unix.Fstatat(parent, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return syscallPathError("verify entry", name, err)
	}
	if current.Mode&unix.S_IFMT == unix.S_IFLNK {
		return &PathError{Op: "verify entry", Path: name, Err: ErrSymlink}
	}
	if uint32(current.Mode)&uint32(unix.S_IFMT) != expectedType || current.Dev != expected.Dev || current.Ino != expected.Ino {
		return &PathError{Op: "verify entry", Path: name, Err: ErrRootChanged}
	}
	return nil
}

func verifyFDIdentity(fd int, expected unix.Stat_t, expectedType uint32, path string) error {
	var current unix.Stat_t
	if err := unix.Fstat(fd, &current); err != nil {
		return syscallPathError("verify open entry", path, err)
	}
	if uint32(current.Mode)&uint32(unix.S_IFMT) != expectedType || current.Dev != expected.Dev || current.Ino != expected.Ino {
		return &PathError{Op: "verify open entry", Path: path, Err: ErrRootChanged}
	}
	return nil
}

func appendPath(path []string, name string) []string {
	result := make([]string, len(path)+1)
	copy(result, path)
	result[len(path)] = name
	return result
}

func (node *removeTreeNode) close() {
	if node == nil {
		return
	}
	for _, child := range node.children {
		child.node.close()
	}
	if node.fd >= 0 {
		_ = unix.Close(node.fd)
		node.fd = -1
	}
}
